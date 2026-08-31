package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Bot struct {
	token        string
	chatID       string
	client       *http.Client
	apiBaseURL   string
	pollRetryMin time.Duration
	pollRetryMax time.Duration
	offset       int64
}

type Callback struct {
	ID        string
	Data      string
	MessageID int64
	ChatID    string
	UserID    string
	Text      string
}

type Button struct {
	Text string
	Data string
}

type apiResp[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

type update struct {
	UpdateID      int64          `json:"update_id"`
	CallbackQuery *callbackQuery `json:"callback_query"`
	Message       *message       `json:"message"`
}

type callbackQuery struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	From    *user  `json:"from"`
	Message *struct {
		MessageID int64 `json:"message_id"`
		Chat      *chat `json:"chat"`
	} `json:"message"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      *chat  `json:"chat"`
	From      *user  `json:"from"`
}

type chat struct {
	ID int64 `json:"id"`
}

type user struct {
	ID int64 `json:"id"`
}

func New(token, chatID string) *Bot {
	return &Bot{token: strings.TrimSpace(token), chatID: strings.TrimSpace(chatID), client: &http.Client{Timeout: 75 * time.Second}}
}

func (b *Bot) Enabled() bool {
	return b != nil && b.token != "" && b.chatID != ""
}

func (b *Bot) authorized(chatID, userID string) bool {
	if b == nil {
		return false
	}
	allowed := strings.TrimPrefix(strings.TrimSpace(b.chatID), "telegram:")
	chatID = strings.TrimPrefix(strings.TrimSpace(chatID), "telegram:")
	userID = strings.TrimPrefix(strings.TrimSpace(userID), "telegram:")
	return allowed != "" && (chatID == allowed || userID == allowed)
}

func (b *Bot) Send(ctx context.Context, text string) error {
	_, err := b.SendMessage(ctx, text, nil)
	return err
}

func (b *Bot) SendUpdatePrompt(ctx context.Context, text, updateData, ignoreData string) (int64, error) {
	return b.SendMessage(ctx, text, Keyboard([][]Button{{
		{Text: "更新", Data: updateData},
		{Text: "忽略", Data: ignoreData},
	}}))
}

func (b *Bot) SendSetupTest(ctx context.Context, text string) (int64, error) {
	return b.SendMainMenu(ctx, text)
}

func (b *Bot) SendMainMenu(ctx context.Context, text string) (int64, error) {
	return b.SendMessage(ctx, text, MainMenuKeyboard())
}

func (b *Bot) SetCommands(ctx context.Context) error {
	if !b.Enabled() {
		return nil
	}
	commands := []map[string]string{
		{"command": "start", "description": "打开 DockUP 主菜单"},
		{"command": "docker", "description": "查看 Docker / Compose 项目"},
		{"command": "checkall", "description": "立即检查全部容器更新"},
		{"command": "settings", "description": "设置自动检查间隔"},
		{"command": "help", "description": "显示帮助和主菜单"},
	}
	return b.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}

func MainMenuKeyboard() map[string]any {
	return Keyboard([][]Button{
		{{Text: "🐳 本机 Docker", Data: "home"}, {Text: "➕ 添加服务器", Data: "addserver"}},
		{{Text: "🌐 远程 VPS", Data: "agents"}},
		{{Text: "检查全部更新", Data: "checkall"}, {Text: "设置间隔", Data: "settings"}},
	})
}

func Keyboard(rows [][]Button) map[string]any {
	inline := make([][]map[string]string, 0, len(rows))
	for _, row := range rows {
		out := make([]map[string]string, 0, len(row))
		for _, btn := range row {
			out = append(out, map[string]string{"text": btn.Text, "callback_data": btn.Data})
		}
		inline = append(inline, out)
	}
	return map[string]any{"inline_keyboard": inline}
}

func (b *Bot) SendMessage(ctx context.Context, text string, replyMarkup any) (int64, error) {
	if !b.Enabled() || strings.TrimSpace(text) == "" {
		return 0, nil
	}
	payload := map[string]any{
		"chat_id":                  b.chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := b.call(ctx, "sendMessage", payload, &result); err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

func (b *Bot) EditMessage(ctx context.Context, messageID int64, text string) error {
	return b.EditMessageWithKeyboard(ctx, messageID, text, nil)
}

func (b *Bot) EditMessageWithKeyboard(ctx context.Context, messageID int64, text string, replyMarkup any) error {
	if !b.Enabled() || messageID == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	if replyMarkup == nil {
		replyMarkup = map[string]any{"inline_keyboard": [][]map[string]string{}}
	}
	payload := map[string]any{
		"chat_id":                  b.chatID,
		"message_id":               messageID,
		"text":                     text,
		"disable_web_page_preview": true,
		"reply_markup":             replyMarkup,
	}
	return b.call(ctx, "editMessageText", payload, nil)
}

func (b *Bot) AnswerCallback(ctx context.Context, callbackID, text string) error {
	if !b.Enabled() || callbackID == "" {
		return nil
	}
	payload := map[string]any{"callback_query_id": callbackID}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
	}
	return b.call(ctx, "answerCallbackQuery", payload, nil)
}

func (b *Bot) PollCallbacks(ctx context.Context, out chan<- Callback) error {
	if !b.Enabled() {
		<-ctx.Done()
		return ctx.Err()
	}
	retryMin := b.pollRetryMin
	if retryMin <= 0 {
		retryMin = 3 * time.Second
	}
	retryMax := b.pollRetryMax
	if retryMax < retryMin {
		retryMax = 30 * time.Second
		if retryMax < retryMin {
			retryMax = retryMin
		}
	}
	retryDelay := retryMin
	consecutiveErrors := 0
	lastErrorLog := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			consecutiveErrors++
			now := time.Now()
			if consecutiveErrors == 1 || now.Sub(lastErrorLog) >= time.Minute {
				slog.Warn("telegram callback polling failed; retrying", "error", b.redactError(err), "failures", consecutiveErrors, "retry_in", retryDelay)
				lastErrorLog = now
			}
			if !waitContext(ctx, retryDelay) {
				return ctx.Err()
			}
			if retryDelay < retryMax {
				retryDelay *= 2
				if retryDelay > retryMax {
					retryDelay = retryMax
				}
			}
			continue
		}
		if consecutiveErrors > 0 {
			slog.Info("telegram callback polling recovered", "failures", consecutiveErrors)
			consecutiveErrors = 0
		}
		retryDelay = retryMin
		for _, u := range updates {
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}
			if u.CallbackQuery != nil && u.CallbackQuery.Data != "" {
				msgID := int64(0)
				chatID := ""
				if u.CallbackQuery.Message != nil {
					msgID = u.CallbackQuery.Message.MessageID
					if u.CallbackQuery.Message.Chat != nil {
						chatID = fmt.Sprintf("%d", u.CallbackQuery.Message.Chat.ID)
					}
				}
				userID := ""
				if u.CallbackQuery.From != nil {
					userID = fmt.Sprintf("%d", u.CallbackQuery.From.ID)
				}
				if !b.authorized(chatID, userID) {
					_ = b.AnswerCallback(ctx, u.CallbackQuery.ID, "未授权")
					continue
				}
				select {
				case out <- Callback{ID: u.CallbackQuery.ID, Data: u.CallbackQuery.Data, MessageID: msgID, ChatID: chatID, UserID: userID}:
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			if u.Message != nil {
				chatID := ""
				if u.Message.Chat != nil {
					chatID = fmt.Sprintf("%d", u.Message.Chat.ID)
				}
				userID := ""
				if u.Message.From != nil {
					userID = fmt.Sprintf("%d", u.Message.From.ID)
				}
				if !b.authorized(chatID, userID) {
					continue
				}
				cmd := normalizeCommand(u.Message.Text)
				data := "msg"
				if cmd != "" {
					data = "cmd:" + cmd
				}
				select {
				case out <- Callback{Data: data, MessageID: u.Message.MessageID, ChatID: chatID, UserID: userID, Text: u.Message.Text}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (b *Bot) redactError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if b != nil && b.token != "" {
		message = strings.ReplaceAll(message, b.token, "[redacted]")
	}
	return message
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (b *Bot) getUpdates(ctx context.Context) ([]update, error) {
	payload := map[string]any{
		"offset":          b.offset,
		"timeout":         60,
		"allowed_updates": []string{"callback_query", "message"},
	}
	var updates []update
	if err := b.call(ctx, "getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func normalizeCommand(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	cmd := strings.Fields(text)[0]
	cmd = strings.TrimPrefix(cmd, "/")
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "start", "help", "menu":
		return "start"
	case "docker", "dockup":
		return "docker"
	case "settings":
		return "settings"
	case "check", "checkall":
		return "checkall"
	default:
		return ""
	}
}

func (b *Bot) call(ctx context.Context, method string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(b.apiBaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	url := fmt.Sprintf("%s/bot%s/%s", baseURL, b.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram %s failed: %s %s", method, resp.Status, strings.TrimSpace(string(respBody)))
	}
	var parsed apiResp[json.RawMessage]
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return err
	}
	if !parsed.OK {
		return fmt.Errorf("telegram %s failed: %s", method, parsed.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(parsed.Result, out)
}
