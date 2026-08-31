package updater

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/config"
	"github.com/shui1iao/DockUP/internal/dockerx"
	"github.com/shui1iao/DockUP/internal/reverse"
	"github.com/shui1iao/DockUP/internal/telegram"
)

type CheckSummary struct {
	LocalChecked         int
	LocalUpdates         int
	RemoteChecked        int
	RemoteUpdates        int
	Errors               int
	ErrorDetails         []string
	Skipped              []string
	UpdateTokens         []string
	PendingUpdateTokens  []string
	PendingLocalUpdates  int
	PendingRemoteUpdates int
	Updates              []pendingUpdate
}

func (s CheckSummary) TotalUpdates() int   { return s.LocalUpdates + s.RemoteUpdates }
func (s CheckSummary) Checked() int        { return s.LocalChecked + s.RemoteChecked }
func (s CheckSummary) PendingUpdates() int { return s.PendingLocalUpdates + s.PendingRemoteUpdates }

type Updater struct {
	cfg             config.Config
	docker          *dockerx.Client
	bot             *telegram.Bot
	agents          *agent.Client
	reverse         *reverse.Client
	store           *config.Store
	log             *slog.Logger
	pending         map[string]pendingUpdate
	manualChecks    map[string]pendingUpdate
	batchUpdates    map[string][]string
	confirmDeletes  map[string]confirmDelete
	confirmAgents   map[string]confirmAgentDelete
	addStates       map[int64]addServerState
	currentInterval time.Duration
	mu              sync.Mutex
}

type pendingUpdate struct {
	Token      string
	Container  dockerx.ContainerInfo
	OldVersion dockerx.ImageVersion
	NewVersion dockerx.ImageVersion
	AgentID    string
	AgentName  string
	MessageID  int64
	CreatedAt  time.Time
}

type confirmDelete struct {
	Token     string
	Project   dockerx.ProjectInfo
	MessageID int64
	CreatedAt time.Time
}

type confirmAgentDelete struct {
	Token     string
	Agent     config.AgentConfig
	MessageID int64
	CreatedAt time.Time
}

type addServerState struct {
	Step      string
	Name      string
	MessageID int64
	CreatedAt time.Time
}

func New(cfg config.Config, docker *dockerx.Client, bot *telegram.Bot, log *slog.Logger, args ...any) *Updater {
	var store *config.Store
	var hub *reverse.Hub
	for _, arg := range args {
		switch v := arg.(type) {
		case *config.Store:
			store = v
		case *reverse.Hub:
			hub = v
		}
	}
	if store == nil {
		var err error
		store, err = config.NewStore(cfg.DataPath)
		if err != nil {
			log.Warn("load DockUP store failed", "error", err)
		}
	}
	agents := cfg.Agents
	if store != nil {
		agents = append(agents, store.Agents()...)
	}
	var reverseClient *reverse.Client
	if hub != nil {
		reverseClient = reverse.NewClient(hub)
	}
	return &Updater{
		cfg:             cfg,
		docker:          docker,
		bot:             bot,
		agents:          agent.NewClient(agents, cfg.AgentToken),
		reverse:         reverseClient,
		store:           store,
		log:             log,
		pending:         map[string]pendingUpdate{},
		manualChecks:    map[string]pendingUpdate{},
		batchUpdates:    map[string][]string{},
		confirmDeletes:  map[string]confirmDelete{},
		confirmAgents:   map[string]confirmAgentDelete{},
		addStates:       map[int64]addServerState{},
		currentInterval: cfg.CheckInterval,
	}
}

func (u *Updater) Run(ctx context.Context) error {
	callbacks := make(chan telegram.Callback, 32)
	if u.bot.Enabled() {
		go u.runCallbackPolling(ctx, callbacks)
	}

	if u.cfg.RunOnce {
		if u.bot.Enabled() {
			u.sendMainMenu(ctx)
		}
		return u.CheckOnce(ctx)
	}

	u.log.Info("DockUP started", "interval", u.currentInterval.String())
	if u.bot.Enabled() {
		u.sendMainMenu(ctx)
	}
	go func() {
		if err := u.CheckOnce(ctx); err != nil {
			u.log.Error("initial check failed", "error", err)
		}
	}()
	pairTicker := time.NewTicker(10 * time.Second)
	defer pairTicker.Stop()

	var ticker *time.Ticker
	var tick <-chan time.Time
	if u.currentInterval > 0 {
		ticker = time.NewTicker(u.currentInterval)
		tick = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cb := <-callbacks:
			u.log.Info("telegram callback received", "action", callbackAction(cb.Data))
			go u.handleCallback(ctx, cb)
		case <-pairTicker.C:
			u.checkPendingPairs(ctx)
		case <-tick:
			if err := u.CheckOnce(ctx); err != nil {
				u.log.Error("scheduled check failed", "error", err)
			}
		}
		if interval := u.interval(); interval != u.currentInterval {
			if ticker != nil {
				ticker.Stop()
			}
			u.currentInterval = interval
			if interval > 0 {
				ticker = time.NewTicker(interval)
				tick = ticker.C
			} else {
				ticker = nil
				tick = nil
			}
		}
	}
}

func (u *Updater) runCallbackPolling(ctx context.Context, callbacks chan<- telegram.Callback) {
	for {
		err := u.bot.PollCallbacks(ctx, callbacks)
		if ctx.Err() != nil {
			return
		}
		u.log.Warn("telegram callback polling stopped; restarting", "error", err)
		if !waitForCallbackRetry(ctx, 3*time.Second) {
			return
		}
	}
}

func waitForCallbackRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func callbackAction(data string) string {
	data = strings.TrimSpace(data)
	if data == "" {
		return "empty"
	}
	if i := strings.IndexByte(data, ':'); i >= 0 {
		return data[:i]
	}
	switch data {
	case "home", "addserver", "agents", "main", "checkall", "settings", "cancel":
		return data
	default:
		return "unknown"
	}
}

func (u *Updater) checkPendingPairs(ctx context.Context) {
	if u.store == nil {
		return
	}
	results, err := u.store.CompleteConnectedPairs()
	if err != nil {
		u.log.Warn("complete connected pair failed", "error", err)
		return
	}
	for _, result := range results {
		u.refreshAgents()
		if u.bot != nil && u.bot.Enabled() {
			text := fmt.Sprintf("✅ 服务器接入成功\n\n名称：%s\n地址：%s\n\n之后会按当前间隔自动检查更新并通过这里通知。", result.Agent.Name, result.Agent.URL)
			if result.MessageID != 0 {
				_ = u.bot.EditMessageWithKeyboard(ctx, result.MessageID, text, telegram.Keyboard([][]telegram.Button{{{Text: "查看远程 VPS", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
			} else {
				_, _ = u.bot.SendMessage(ctx, text, telegram.Keyboard([][]telegram.Button{{{Text: "查看远程 VPS", Data: "agents"}}}))
			}
		}
	}
}

func (u *Updater) CheckOnce(parent context.Context) error {
	_, err := u.CheckOnceSummary(parent, false)
	return err
}

func (u *Updater) CheckOnceSummary(parent context.Context, manual bool) (CheckSummary, error) {
	ctx, cancel := context.WithTimeout(parent, u.cfg.Timeout)
	defer cancel()
	summary := CheckSummary{}
	if u.interval() <= 0 {
		u.log.Info("automatic update check skipped", "interval", u.interval().String())
		return summary, nil
	}

	if u.cfg.CheckLocal {
		containers, err := u.docker.RunningContainers(ctx)
		if err != nil {
			return summary, err
		}
		summary.LocalChecked = len(containers)
		u.log.Info("checking containers", "count", len(containers))

		for _, c := range containers {
			token, update, skipped, err := u.checkContainer(ctx, c, manual)
			if err != nil {
				summary.Errors++
				summary.ErrorDetails = append(summary.ErrorDetails, fmt.Sprintf("本机 %s：%s", c.Name, friendlyError(err)))
				u.log.Warn("container check failed", "container", c.Name, "image", c.Image, "error", err)
				continue
			}
			if skipped != "" {
				summary.Skipped = append(summary.Skipped, skipped)
				continue
			}
			if token != "" || update != nil {
				summary.LocalUpdates++
			}
			if token != "" {
				summary.UpdateTokens = append(summary.UpdateTokens, token)
			}
			if update != nil {
				summary.Updates = append(summary.Updates, *update)
			}
		}
	} else {
		u.log.Info("local update check skipped")
	}
	remote, err := u.CheckRemoteAgents(ctx, manual)
	summary.RemoteChecked += remote.RemoteChecked
	summary.RemoteUpdates += remote.RemoteUpdates
	summary.Errors += remote.Errors
	summary.Skipped = append(summary.Skipped, remote.Skipped...)
	summary.ErrorDetails = append(summary.ErrorDetails, remote.ErrorDetails...)
	summary.UpdateTokens = append(summary.UpdateTokens, remote.UpdateTokens...)
	summary.Updates = append(summary.Updates, remote.Updates...)
	if !manual {
		summary.PendingUpdateTokens, summary.PendingLocalUpdates, summary.PendingRemoteUpdates = u.pendingUpdateSnapshot()
	}
	return summary, err
}

func (u *Updater) pendingUpdateSnapshot() ([]string, int, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	tokens := make([]string, 0, len(u.pending))
	local := 0
	remote := 0
	for token, p := range u.pending {
		tokens = append(tokens, token)
		if p.AgentID != "" {
			remote++
		} else {
			local++
		}
	}
	return tokens, local, remote
}

func (u *Updater) CheckRemoteAgents(ctx context.Context, manual bool) (CheckSummary, error) {
	summary := CheckSummary{}
	if u.agents == nil || !u.agents.Enabled() {
		return summary, nil
	}
	for _, a := range u.agents.Agents() {
		agentCfg := a
		var updates []agent.UpdateInfo
		var err error
		if a.Mode == "reverse" && u.reverse != nil {
			updates, err = u.reverse.CheckUpdates(ctx, a)
		} else {
			agentCfg, updates, err = u.agents.CheckUpdates(ctx, a.ID)
		}
		if err != nil {
			summary.Errors++
			summary.ErrorDetails = append(summary.ErrorDetails, fmt.Sprintf("远程 %s：%s", a.Name, friendlyError(err)))
			u.log.Warn("remote agent update check failed", "agent", a.Name, "error", err)
			continue
		}
		summary.RemoteChecked++
		summary.RemoteUpdates += len(updates)
		u.log.Info("remote agent update check finished", "agent", agentCfg.Name, "updates", len(updates))
		for _, up := range updates {
			if manual {
				summary.Updates = append(summary.Updates, pendingUpdate{Token: randomToken(), Container: up.Container, OldVersion: up.OldVersion, NewVersion: up.NewVersion, AgentID: agentCfg.ID, AgentName: agentCfg.Name, CreatedAt: time.Now()})
				continue
			}
			token, err := u.notifyRemoteUpdate(ctx, agentCfg.ID, agentCfg.Name, up.Container, up.OldVersion, up.NewVersion)
			if err != nil {
				u.log.Warn("remote update notification failed", "agent", agentCfg.Name, "container", up.Container.Name, "error", err)
				continue
			}
			if token != "" {
				summary.UpdateTokens = append(summary.UpdateTokens, token)
			}
		}
	}
	return summary, nil
}

func (u *Updater) checkContainer(ctx context.Context, c dockerx.ContainerInfo, manual bool) (token string, update *pendingUpdate, skipped string, err error) {
	oldVersion, err := u.docker.InspectImageVersionByIDWithRef(ctx, c.ImageID, c.Image)
	if err != nil {
		return "", nil, "", err
	}
	if oldVersion.ID == "" {
		oldVersion.ID = c.ImageID
	}

	u.log.Info("pulling image", "container", c.Name, "image", c.Image)
	newVersion, err := u.docker.RemoteImageVersion(ctx, c.Image)
	if err != nil {
		if isPullUnavailable(err) {
			u.log.Info("container image is not pullable, skipped", "container", c.Name, "image", c.Image)
			return "", nil, c.Name + "（本地镜像，无法从仓库 pull）", nil
		}
		return "", nil, "", err
	}
	if dockerx.SameEffectiveImage(oldVersion, newVersion) {
		return "", nil, "", nil
	}

	if manual {
		up := pendingUpdate{Token: randomToken(), Container: c, OldVersion: oldVersion, NewVersion: newVersion, CreatedAt: time.Now()}
		return "", &up, "", nil
	}
	token, err = u.notifyUpdate(ctx, c, oldVersion, newVersion)
	return token, nil, "", err
}

func isPullUnavailable(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "pull access denied") || strings.Contains(msg, "repository does not exist") || strings.Contains(msg, "not found")
}

func (u *Updater) notifyUpdate(ctx context.Context, c dockerx.ContainerInfo, oldVersion, newVersion dockerx.ImageVersion) (string, error) {
	return u.notifyUpdateFrom(ctx, "", "", c, oldVersion, newVersion)
}

func (u *Updater) notifyRemoteUpdate(ctx context.Context, agentID, agentName string, c dockerx.ContainerInfo, oldVersion, newVersion dockerx.ImageVersion) (string, error) {
	return u.notifyUpdateFrom(ctx, agentID, agentName, c, oldVersion, newVersion)
}

func (u *Updater) notifyUpdateFrom(ctx context.Context, agentID, agentName string, c dockerx.ContainerInfo, oldVersion, newVersion dockerx.ImageVersion) (string, error) {
	if !u.bot.Enabled() {
		u.log.Warn("update available but telegram is not configured", "agent", agentName, "container", c.Name, "image", c.Image, "old", oldVersion.Display(), "new", newVersion.Display())
		return "", nil
	}
	u.mu.Lock()
	for _, p := range u.pending {
		if p.AgentID == agentID && p.Container.ID == c.ID && normalizeID(p.NewVersion.ID) == normalizeID(newVersion.ID) {
			u.mu.Unlock()
			return p.Token, nil
		}
	}
	token := randomToken()
	p := pendingUpdate{Token: token, Container: c, OldVersion: oldVersion, NewVersion: newVersion, AgentID: agentID, AgentName: agentName, CreatedAt: time.Now()}
	u.pending[token] = p
	u.mu.Unlock()

	text := formatPrompt(agentName, c, oldVersion, newVersion)
	msgID, err := u.bot.SendUpdatePrompt(ctx, text, "update:"+token, "ignore:"+token)
	if err != nil {
		return "", err
	}
	if msgID != 0 {
		u.mu.Lock()
		p.MessageID = msgID
		u.pending[token] = p
		u.mu.Unlock()
	}
	return token, nil
}

func (u *Updater) handleCallback(parent context.Context, cb telegram.Callback) {
	if u.handleManageCallback(parent, cb) {
		return
	}
	parts := strings.SplitN(cb.Data, ":", 2)
	if len(parts) != 2 {
		_ = u.bot.AnswerCallback(parent, cb.ID, "操作无效或按钮版本过旧")
		u.log.Warn("unhandled telegram callback", "action", callbackAction(cb.Data))
		return
	}
	action, token := parts[0], parts[1]
	if action == "noop" {
		_ = u.bot.AnswerCallback(parent, cb.ID, "测试按钮，无操作")
		return
	}
	if action != "ignore" && action != "update" {
		_ = u.bot.AnswerCallback(parent, cb.ID, "操作无效或按钮版本过旧")
		u.log.Warn("unhandled telegram callback", "action", callbackAction(cb.Data))
		return
	}

	u.mu.Lock()
	p, ok := u.pending[token]
	if ok {
		delete(u.pending, token)
	}
	u.mu.Unlock()

	if !ok {
		_ = u.bot.AnswerCallback(parent, cb.ID, "这个更新已经处理或过期")
		return
	}
	if p.MessageID == 0 {
		p.MessageID = cb.MessageID
	}

	switch action {
	case "ignore":
		_ = u.bot.AnswerCallback(parent, cb.ID, "已忽略")
		_ = u.bot.EditMessage(parent, p.MessageID, formatIgnored(p))
	case "update":
		_ = u.bot.AnswerCallback(parent, cb.ID, "开始更新")
		_ = u.bot.EditMessage(parent, p.MessageID, formatUpdating(p))
		go u.applyUpdate(context.Background(), p)
	}
}

func (u *Updater) applyUpdate(parent context.Context, p pendingUpdate) {
	if parent == nil {
		parent = context.Background()
	}
	if p.Container.Name == "dockup" {
		ctx, cancel := context.WithTimeout(parent, u.cfg.Timeout)
		defer cancel()
		u.log.Info("updating container", "container", p.Container.Name, "image", p.Container.Image, "old", p.OldVersion.Display(), "new", p.NewVersion.Display())
		helperID, err := u.docker.RunSelfUpdateHelper(ctx, p.Container.Image, p.Container.ID, p.Container.Image, u.cfg.Cleanup)
		if err != nil {
			_ = u.bot.EditMessage(parent, p.MessageID, formatFailed(p, err))
			return
		}
		_ = u.bot.EditMessage(parent, p.MessageID, formatSelfUpdateStarted(p, helperID))
		return
	}
	if err := u.applyUpdateOnce(parent, p); err != nil {
		u.log.Error("container update failed", "container", p.Container.Name, "image", p.Container.Image, "error", err)
		u.editUpdateMessage(formatFailed(p, err), p.MessageID)
		return
	}
	u.log.Info("container update finished", "container", p.Container.Name, "image", p.Container.Image, "old", p.OldVersion.Display(), "new", p.NewVersion.Display())
	u.editUpdateMessage(formatSuccess(p), p.MessageID)
}

func (u *Updater) editUpdateMessage(text string, messageID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := u.bot.EditMessage(ctx, messageID, text); err != nil {
		u.log.Error("failed to edit update message", "message_id", messageID, "error", err)
	}
}

func (u *Updater) applyUpdateOnce(parent context.Context, p pendingUpdate) error {
	ctx, cancel := context.WithTimeout(parent, u.cfg.Timeout)
	defer cancel()

	u.log.Info("updating container", "container", p.Container.Name, "image", p.Container.Image, "old", p.OldVersion.Display(), "new", p.NewVersion.Display())
	if p.Container.Name == "dockup" {
		_, err := u.docker.RunSelfUpdateHelper(ctx, p.Container.Image, p.Container.ID, p.Container.Image, u.cfg.Cleanup)
		return err
	}
	if p.AgentID != "" {
		agentCfg, ok := u.findAgent(p.AgentID)
		if ok && agentCfg.Mode == "reverse" && u.reverse != nil {
			return u.reverse.UpdateContainer(ctx, agentCfg, p.Container.ID, p.Container.Image, u.cfg.Cleanup)
		}
		return u.agents.UpdateContainer(ctx, p.AgentID, p.Container.ID, p.Container.Image, u.cfg.Cleanup)
	}
	_, _, err := u.docker.UpdateContainer(ctx, p.Container.ID, p.Container.Image, u.cfg.Cleanup)
	return err
}

func (u *Updater) findAgent(id string) (config.AgentConfig, bool) {
	if u.agents == nil {
		return config.AgentConfig{}, false
	}
	for _, a := range u.agents.Agents() {
		if a.ID == id {
			return a, true
		}
	}
	return config.AgentConfig{}, false
}

func formatPrompt(agentName string, c dockerx.ContainerInfo, oldVersion, newVersion dockerx.ImageVersion) string {
	head := "🐳 发现 Docker 镜像更新"
	if agentName != "" {
		head += "\n\n服务器：" + agentName
	}
	return fmt.Sprintf("%s\n\n容器：%s\n镜像：%s\n旧版本：%s\n新版本：%s\n\n请选择是否更新。", head, c.Name, c.Image, oldVersion.Display(), newVersion.Display())
}

func formatUpdating(p pendingUpdate) string {
	return fmt.Sprintf("⏳ 正在更新 Docker 容器\n\n%s容器：%s\n镜像：%s\n版本：%s → %s", serverLine(p), p.Container.Name, p.Container.Image, p.OldVersion.Display(), p.NewVersion.Display())
}

func formatSuccess(p pendingUpdate) string {
	return fmt.Sprintf("✅ Docker 容器更新成功\n\n%s容器：%s\n镜像：%s\n版本：%s → %s", serverLine(p), p.Container.Name, p.Container.Image, p.OldVersion.Display(), p.NewVersion.Display())
}

func formatSelfUpdateStarted(p pendingUpdate, helperID string) string {
	return fmt.Sprintf("🔄 DockUP 自更新已交给临时容器执行\n\n容器：%s\n镜像：%s\n版本：%s → %s\n临时容器：%s\n\n如果更新成功，DockUP 会以新镜像重新启动。", p.Container.Name, p.Container.Image, p.OldVersion.Display(), p.NewVersion.Display(), shortID(helperID))
}

func formatFailed(p pendingUpdate, err error) string {
	return fmt.Sprintf("❌ Docker 容器更新失败\n\n%s容器：%s\n镜像：%s\n版本：%s → %s\n错误：%s", serverLine(p), p.Container.Name, p.Container.Image, p.OldVersion.Display(), p.NewVersion.Display(), err.Error())
}

func formatIgnored(p pendingUpdate) string {
	return fmt.Sprintf("⏭️ 已忽略 Docker 镜像更新\n\n%s容器：%s\n镜像：%s\n版本：%s → %s", serverLine(p), p.Container.Name, p.Container.Image, p.OldVersion.Display(), p.NewVersion.Display())
}

func serverLine(p pendingUpdate) string {
	if p.AgentName == "" {
		return ""
	}
	return "服务器：" + p.AgentName + "\n"
}

func randomToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func normalizeID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "sha256:")
}

func shortID(id string) string {
	id = normalizeID(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
