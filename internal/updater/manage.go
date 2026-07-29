package updater

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shui1iao/DockUP/internal/agent"
	"github.com/shui1iao/DockUP/internal/config"
	"github.com/shui1iao/DockUP/internal/dockerx"
	"github.com/shui1iao/DockUP/internal/telegram"
)

func (u *Updater) handleManageCallback(ctx context.Context, cb telegram.Callback) bool {
	data := cb.Data
	if strings.HasPrefix(data, "cmd:") {
		return u.handleCommand(ctx, cb, strings.TrimPrefix(data, "cmd:"))
	}
	if data == "msg" {
		return u.handleTextMessage(ctx, cb)
	}
	if data == "home" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "")
		u.showHome(ctx, cb.MessageID)
		return true
	}
	if data == "addserver" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "")
		u.startAddServer(ctx, cb.MessageID)
		return true
	}
	if data == "agents" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "正在获取远程 VPS")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "⏳ 正在获取远程 VPS 状态…", nil)
		go u.showAgents(ctx, cb.MessageID)
		return true
	}
	if strings.HasPrefix(data, "agent:") {
		agentID := strings.TrimPrefix(data, "agent:")
		_ = u.bot.AnswerCallback(ctx, cb.ID, "正在获取远程 VPS")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "⏳ 正在获取远程 VPS 状态…", nil)
		go u.showAgent(ctx, cb.MessageID, agentID)
		return true
	}
	if strings.HasPrefix(data, "adelask:") {
		u.confirmAgentDelete(ctx, cb, strings.TrimPrefix(data, "adelask:"))
		return true
	}
	if strings.HasPrefix(data, "adelcheck:") {
		u.executeAgentDelete(ctx, cb, strings.TrimPrefix(data, "adelcheck:"))
		return true
	}
	if strings.HasPrefix(data, "adelconfirm:") {
		u.forceAgentDelete(ctx, cb, strings.TrimPrefix(data, "adelconfirm:"))
		return true
	}
	if strings.HasPrefix(data, "rproject:") {
		raw := strings.TrimPrefix(data, "rproject:")
		_ = u.bot.AnswerCallback(ctx, cb.ID, "正在获取项目")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "⏳ 正在获取远程项目状态…", nil)
		go u.showRemoteProject(ctx, cb.MessageID, raw)
		return true
	}
	if strings.HasPrefix(data, "acheck:") {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "开始检查")
		u.manualRemoteAgentCheck(ctx, cb.MessageID, strings.TrimPrefix(data, "acheck:"))
		return true
	}
	if strings.HasPrefix(data, "rcheck:") {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "开始检查")
		u.manualRemoteProjectCheck(ctx, cb.MessageID, strings.TrimPrefix(data, "rcheck:"))
		return true
	}
	if strings.HasPrefix(data, "rpstart:") || strings.HasPrefix(data, "rpstop:") || strings.HasPrefix(data, "rprestart:") {
		u.handleRemoteProjectAction(ctx, cb)
		return true
	}
	if data == "main" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "")
		u.showMainMenu(ctx, cb.MessageID)
		return true
	}
	if data == "checkall" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "开始检查全部")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "⏳ 正在检查全部容器更新…", nil)
		go func() {
			summary, err := u.CheckOnceSummary(ctx, true)
			if err != nil {
				_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, friendlyErrorText("❌ 检查失败", err), navKeyboard())
				return
			}
			_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, checkAllDoneText(summary), u.checkAllDoneKeyboard(summary))
		}()
		return true
	}
	if data == "settings" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "")
		u.showSettings(ctx, cb.MessageID)
		return true
	}
	if strings.HasPrefix(data, "interval:") {
		u.handleSetInterval(ctx, cb, strings.TrimPrefix(data, "interval:"))
		return true
	}
	if strings.HasPrefix(data, "project:") {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "")
		u.showProject(ctx, cb.MessageID, strings.TrimPrefix(data, "project:"))
		return true
	}
	if strings.HasPrefix(data, "pcheck:") {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "开始检查")
		u.manualProjectCheck(ctx, cb.MessageID, strings.TrimPrefix(data, "pcheck:"))
		return true
	}
	if strings.HasPrefix(data, "pupdate:") {
		u.handleManualUpdate(ctx, cb, strings.TrimPrefix(data, "pupdate:"))
		return true
	}
	if strings.HasPrefix(data, "batchupdate:") {
		u.handleBatchUpdate(ctx, cb, strings.TrimPrefix(data, "batchupdate:"))
		return true
	}
	if strings.HasPrefix(data, "pstart:") || strings.HasPrefix(data, "pstop:") || strings.HasPrefix(data, "prestart:") {
		u.handleProjectAction(ctx, cb)
		return true
	}
	if strings.HasPrefix(data, "pdelete:") {
		u.confirmProjectDelete(ctx, cb, strings.TrimPrefix(data, "pdelete:"))
		return true
	}
	if strings.HasPrefix(data, "delok:") {
		u.executeProjectDelete(ctx, cb, strings.TrimPrefix(data, "delok:"))
		return true
	}
	if data == "cancel" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "已取消")
		u.showHome(ctx, cb.MessageID)
		return true
	}
	return false
}

func (u *Updater) handleCommand(ctx context.Context, cb telegram.Callback, cmd string) bool {
	switch cmd {
	case "start":
		u.sendMainMenu(ctx)
	case "docker":
		u.sendHome(ctx)
	case "settings":
		msg, _ := u.bot.SendMessage(ctx, "⚙️ 自动检查间隔", nil)
		u.showSettings(ctx, msg)
	case "checkall":
		msg, _ := u.bot.SendMessage(ctx, "⏳ 正在检查全部容器更新…", nil)
		go func() {
			summary, err := u.CheckOnceSummary(ctx, true)
			if err != nil {
				_ = u.bot.EditMessageWithKeyboard(ctx, msg, friendlyErrorText("❌ 检查失败", err), navKeyboard())
				return
			}
			_ = u.bot.EditMessageWithKeyboard(ctx, msg, checkAllDoneText(summary), u.checkAllDoneKeyboard(summary))
		}()
	}
	return true
}

func checkAllDoneText(summary CheckSummary) string {
	lines := []string{"✅ 全部容器检查完成", "", fmt.Sprintf("已检查：本机 %d 个容器 · 远程 %d 台 VPS", summary.LocalChecked, summary.RemoteChecked)}
	if summary.TotalUpdates() > 0 {
		lines = append(lines, fmt.Sprintf("发现更新：%d 个（本机 %d · 远程 %d）", summary.TotalUpdates(), summary.LocalUpdates, summary.RemoteUpdates), "可点下方按钮批量更新，或逐个更新。")
	} else {
		lines = append(lines, "没有发现更新。")
	}
	if len(summary.Skipped) > 0 {
		lines = append(lines, "", fmt.Sprintf("跳过：%d 个", len(summary.Skipped)))
		for i, item := range summary.Skipped {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("…还有 %d 个", len(summary.Skipped)-i))
				break
			}
			lines = append(lines, "• "+item)
		}
	}
	if summary.Errors > 0 {
		lines = append(lines, "", fmt.Sprintf("检查失败：%d 个", summary.Errors))
		for i, item := range summary.ErrorDetails {
			if i >= 3 {
				lines = append(lines, fmt.Sprintf("…还有 %d 个错误", len(summary.ErrorDetails)-i))
				break
			}
			lines = append(lines, "• "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func (u *Updater) checkAllDoneKeyboard(summary CheckSummary) map[string]any {
	rows := [][]telegram.Button{}
	if len(summary.Updates) > 0 {
		tokens := make([]string, 0, len(summary.Updates))
		u.mu.Lock()
		for _, up := range summary.Updates {
			token := up.Token
			if token == "" {
				token = randomToken()
			}
			up.Token = token
			u.manualChecks[token] = up
			tokens = append(tokens, token)
			rows = append(rows, []telegram.Button{{Text: "更新 " + updateButtonLabel(up), Data: "pupdate:" + token}})
		}
		batchToken := randomToken()
		u.batchUpdates[batchToken] = append([]string(nil), tokens...)
		u.mu.Unlock()
		rows = append([][]telegram.Button{{{Text: fmt.Sprintf("全部更新（%d）", len(tokens)), Data: "batchupdate:" + batchToken}}}, rows...)
	}
	rows = append(rows, []telegram.Button{{Text: "返回主界面", Data: "main"}})
	return telegram.Keyboard(rows)
}

func updateButtonLabel(up pendingUpdate) string {
	if up.AgentName != "" {
		return up.AgentName + " / " + up.Container.Name
	}
	return up.Container.Name
}

func (u *Updater) sendMainMenu(ctx context.Context) {
	_, _ = u.bot.SendMainMenu(ctx, mainMenuText(u.interval()))
}

func (u *Updater) showMainMenu(ctx context.Context, messageID int64) {
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, mainMenuText(u.interval()), telegram.MainMenuKeyboard())
}

func mainMenuText(interval time.Duration) string {
	return fmt.Sprintf("🐳 DockUP\n\n轻量 Telegram Docker 管理工具\n自动检查间隔：%s\n\n默认只管理本机；可通过“添加服务器”生成安装命令，把其他 VPS 接入后统一管理和通知。", interval.String())
}

func (u *Updater) sendHome(ctx context.Context) {
	msg, _ := u.bot.SendMessage(ctx, "🐳 Docker 管理", nil)
	u.showHome(ctx, msg)
}

func (u *Updater) showHome(ctx context.Context, messageID int64) {
	projects, err := u.docker.Projects(ctx)
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 获取 Docker 项目失败", err), navKeyboard())
		return
	}
	running := 0
	for _, p := range projects {
		for _, c := range p.Containers {
			if c.State == "running" {
				running++
			}
		}
	}
	text := fmt.Sprintf("🐳 Docker 管理\n\n项目：%d 个 · 运行中：%d 个容器\n自动检查：%s\n\n选择项目查看状态和操作。", len(projects), running, u.interval().String())
	rows := [][]telegram.Button{}
	projectButtons := []telegram.Button{}
	for _, p := range projects {
		icon := "🐳"
		if p.Type == "compose" {
			icon = "📦"
		}
		label := fmt.Sprintf("%s %s", icon, p.Name)
		if len(p.Containers) > 1 {
			label = fmt.Sprintf("%s · %d", label, len(p.Containers))
		}
		projectButtons = append(projectButtons, telegram.Button{Text: label, Data: "project:" + p.Key})
		if len(projectButtons) == 2 {
			rows = append(rows, projectButtons)
			projectButtons = []telegram.Button{}
		}
	}
	if len(projectButtons) > 0 {
		rows = append(rows, projectButtons)
	}
	rows = append(rows, []telegram.Button{{Text: "➕ 添加服务器", Data: "addserver"}, {Text: "🌐 远程 VPS", Data: "agents"}})
	rows = append(rows, []telegram.Button{{Text: "检查全部更新", Data: "checkall"}, {Text: "设置间隔", Data: "settings"}})
	rows = append(rows, []telegram.Button{{Text: "返回主界面", Data: "main"}})
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
}

func (u *Updater) refreshAgents() {
	if u.store == nil || u.agents == nil {
		return
	}
	agents := append([]config.AgentConfig(nil), u.cfg.Agents...)
	agents = append(agents, u.store.Agents()...)
	u.agents.SetAgents(agents)
}

func (u *Updater) startAddServer(ctx context.Context, messageID int64) {
	u.mu.Lock()
	u.addStates[messageID] = addServerState{Step: "name", MessageID: messageID, CreatedAt: time.Now()}
	u.mu.Unlock()
	text := "➕ 添加服务器\n\n请发送服务器名称。\n\n不想填名称就直接发送目标服务器 IP，之后会默认用 IP 作为名称。"
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard([][]telegram.Button{{{Text: "取消", Data: "cancel"}}}))
}

func (u *Updater) handleTextMessage(ctx context.Context, cb telegram.Callback) bool {
	u.mu.Lock()
	var key int64
	var st addServerState
	ok := false
	for k, v := range u.addStates {
		key, st, ok = k, v, true
		break
	}
	u.mu.Unlock()
	if !ok {
		return false
	}
	text := strings.TrimSpace(cb.Text)
	if text == "" {
		_, _ = u.bot.SendMessage(ctx, "内容为空，重新发送一下。", nil)
		return true
	}
	if strings.HasPrefix(text, "/") {
		return false
	}
	if st.Step == "name" {
		if looksLikeAgentAddress(text) {
			u.createPairFromText(ctx, key, st, "", text)
			return true
		}
		st.Name = text
		st.Step = "url"
		u.mu.Lock()
		u.addStates[key] = st
		u.mu.Unlock()
		_, _ = u.bot.SendMessage(ctx, "继续发送服务器 IP 或 Agent 访问地址。\n\n例如：\n1.2.3.4\n或\nhttp://1.2.3.4:8748", nil)
		return true
	}
	if st.Step == "url" {
		u.createPairFromText(ctx, key, st, st.Name, text)
		return true
	}
	return false
}

func (u *Updater) createPairFromText(ctx context.Context, stateKey int64, _ addServerState, name, rawURL string) {
	if u.store == nil {
		_, _ = u.bot.SendMessage(ctx, "❌ 存储未初始化，无法添加服务器。", nil)
		return
	}
	agentURL := config.BuildAgentURL(rawURL)
	if strings.TrimSpace(name) == "" {
		name = config.DefaultAgentName(rawURL)
	}
	pair, err := u.store.CreatePair(name, agentURL, 30*time.Minute)
	if err != nil {
		_, _ = u.bot.SendMessage(ctx, "❌ 添加失败："+friendlyError(err), nil)
		return
	}
	u.mu.Lock()
	delete(u.addStates, stateKey)
	u.mu.Unlock()
	install := u.installCommand(pair)
	msgID, _ := u.bot.SendMessage(ctx, fmt.Sprintf("🧩 服务器：%s\n地址：%s\n\n在目标服务器执行下面命令完成接入：\n\n%s\n\n有效期：30 分钟。对接成功后我会通知。", pair.Name, pair.URL, install), telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
	_ = u.store.SetPairMessage(pair.ID, msgID)
}

func looksLikeAgentAddress(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.Contains(text, "://") {
		return true
	}
	if strings.ContainsAny(text, ".:") && !strings.Contains(text, " ") {
		return true
	}
	return false
}

func (u *Updater) installCommand(pair config.PendingPair) string {
	image := "ghcr.io/shui1iao/dockup:latest"
	center := u.cfg.PublicURL
	if center == "" {
		center = "http://<中心端IP或域名>:8748"
	}
	return fmt.Sprintf("```bash\nmkdir -p /opt/dockup && cd /opt/dockup && cat > docker-compose.yml <<'YAML'\nservices:\n  dockup-agent:\n    image: %s\n    container_name: dockup-agent\n    restart: unless-stopped\n    environment:\n      TZ: Asia/Shanghai\n      DOCKUP_MODE: agent\n      DOCKUP_NAME: %s\n      DOCKUP_PUBLIC_URL: %s\n      DOCKUP_AGENT_TOKEN: %s\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\nYAML\ndocker compose pull && docker compose up -d\n```", image, yamlQuote(pair.Name), yamlQuote(center), yamlQuote(pair.Token))
}

func yamlQuote(s string) string {
	return strconv.Quote(s)
}

func (u *Updater) showAgents(ctx context.Context, messageID int64) {
	u.refreshAgents()
	if u.agents == nil || !u.agents.Enabled() {
		rows := [][]telegram.Button{{{Text: "➕ 添加服务器", Data: "addserver"}}, {{Text: "本机 Docker", Data: "home"}, {Text: "返回主界面", Data: "main"}}}
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "🌐 远程 VPS\n\n还没有添加服务器。点击“添加服务器”生成安装命令。", telegram.Keyboard(rows))
		return
	}
	agents := u.agents.Agents()
	lines := []string{"🌐 远程 VPS", "", fmt.Sprintf("已配置：%d 台", len(agents)), ""}
	rows := [][]telegram.Button{}
	for _, a := range agents {
		var snap agent.Snapshot
		var err error
		agentCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		if a.Mode == "reverse" && u.reverse != nil {
			snap, err = u.reverse.Snapshot(agentCtx, a)
		} else {
			_, snap, err = u.agents.Snapshot(agentCtx, a.ID)
		}
		cancel()
		if err != nil {
			lines = append(lines, fmt.Sprintf("🔴 %s：连接失败", a.Name), "错误："+friendlyError(err), "")
		} else {
			lines = append(lines, fmt.Sprintf("🟢 %s：%d 个项目 · %d/%d 容器运行中", a.Name, snap.Totals.Projects, snap.Totals.Running, snap.Totals.Containers))
		}
		rows = append(rows, []telegram.Button{{Text: a.Name, Data: "agent:" + a.ID}})
	}
	rows = append(rows, []telegram.Button{{Text: "➕ 添加服务器", Data: "addserver"}, {Text: "刷新", Data: "agents"}})
	rows = append(rows, []telegram.Button{{Text: "本机 Docker", Data: "home"}})
	rows = append(rows, []telegram.Button{{Text: "返回主界面", Data: "main"}})
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, strings.Join(lines, "\n"), telegram.Keyboard(rows))
}

func (u *Updater) showAgent(ctx context.Context, messageID int64, agentID string) {
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程 VPS 不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
		return
	}
	var snap agent.Snapshot
	var err error
	agentCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if agentCfg.Mode == "reverse" && u.reverse != nil {
		snap, err = u.reverse.Snapshot(agentCtx, agentCfg)
	} else {
		_, snap, err = u.agents.Snapshot(agentCtx, agentID)
	}
	if err != nil {
		rows := [][]telegram.Button{
			{{Text: "🗑 删除服务器", Data: "adelask:" + agentID}},
			{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}},
		}
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 获取远程 VPS 失败", err), telegram.Keyboard(rows))
		return
	}
	lines := []string{fmt.Sprintf("🌐 %s", snap.Name), "", fmt.Sprintf("项目：%d 个 · 运行中：%d/%d 个容器", snap.Totals.Projects, snap.Totals.Running, snap.Totals.Containers), "", "选择项目查看状态和操作。"}
	rows := [][]telegram.Button{}
	projectButtons := []telegram.Button{}
	for _, p := range snap.Projects {
		icon := "🐳"
		if p.Type == "compose" {
			icon = "📦"
		}
		label := fmt.Sprintf("%s %s", icon, p.Name)
		if len(p.Containers) > 1 {
			label = fmt.Sprintf("%s · %d", label, len(p.Containers))
		}
		projectButtons = append(projectButtons, telegram.Button{Text: label, Data: "rproject:" + agentID + ":" + p.Key})
		if len(projectButtons) == 2 {
			rows = append(rows, projectButtons)
			projectButtons = []telegram.Button{}
		}
	}
	if len(projectButtons) > 0 {
		rows = append(rows, projectButtons)
	}
	rows = append(rows, []telegram.Button{{Text: "检查更新", Data: "acheck:" + agentID}, {Text: "刷新", Data: "agent:" + agentID}})
	rows = append(rows, []telegram.Button{{Text: "🗑 删除服务器", Data: "adelask:" + agentID}})
	rows = append(rows, []telegram.Button{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}})
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, strings.Join(lines, "\n"), telegram.Keyboard(rows))
}

func (u *Updater) confirmAgentDelete(ctx context.Context, cb telegram.Callback, agentID string) {
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "服务器不存在")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "❌ 远程 VPS 不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
		return
	}
	if u.store == nil {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "当前配置不支持删除")
		return
	}
	token := randomToken()
	u.mu.Lock()
	u.confirmAgents[token] = confirmAgentDelete{Token: token, Agent: agentCfg, MessageID: cb.MessageID, CreatedAt: time.Now()}
	u.mu.Unlock()
	cmd := "cd /opt/dockup && docker compose down"
	cleanCmd := "rm -rf /opt/dockup"
	text := fmt.Sprintf("⚠️ 删除远程 VPS？\n\n服务器：%s\n\n请先在这台远端服务器上执行：\n\n```bash\n%s\n```\n\n这只会停止并删除 dockup-agent 容器，不会删除数据目录。\n如果确认 `/opt/dockup` 里没有你要保留的内容，再单独执行清理命令：\n\n```bash\n%s\n```\n\n执行完成后，点下面按钮。DockUP 会检查 Agent 是否已经离线；确认连不上后才会备份并删除本机 JSON 里的服务器记录。\n\n如果这台机器已经没了，也可以强制只删除本机记录。", agentCfg.Name, cmd, cleanCmd)
	rows := [][]telegram.Button{
		{{Text: "我已执行，检查并删除", Data: "adelcheck:" + token}},
		{{Text: "强制只删除本机记录", Data: "adelconfirm:" + token}},
		{{Text: "取消", Data: "agent:" + agentID}},
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "请先在远端执行卸载命令")
	_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, text, telegram.Keyboard(rows))
}

func (u *Updater) executeAgentDelete(ctx context.Context, cb telegram.Callback, token string) {
	p, ok := u.getPendingAgentDelete(token)
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "删除确认已过期")
		return
	}
	if u.agentStillOnline(ctx, p.Agent) {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "Agent 仍在线，未删除")
		text := fmt.Sprintf("⚠️ Agent 仍然在线，暂不删除本机记录。\n\n服务器：%s\n\n请确认已在远端执行卸载命令，或远端机器已关停后再检查。", p.Agent.Name)
		rows := [][]telegram.Button{
			{{Text: "再次检查并删除", Data: "adelcheck:" + token}},
			{{Text: "强制只删除本机记录", Data: "adelconfirm:" + token}},
			{{Text: "取消", Data: "agent:" + p.Agent.ID}},
		}
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, text, telegram.Keyboard(rows))
		return
	}
	u.removeAgentRecord(ctx, cb, token, p.Agent, "✅ Agent 已离线，已删除本机记录")
}

func (u *Updater) forceAgentDelete(ctx context.Context, cb telegram.Callback, token string) {
	p, ok := u.getPendingAgentDelete(token)
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "删除确认已过期")
		return
	}
	u.removeAgentRecord(ctx, cb, token, p.Agent, "✅ 已强制删除本机记录")
}

func (u *Updater) getPendingAgentDelete(token string) (confirmAgentDelete, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	p, ok := u.confirmAgents[token]
	if !ok || time.Since(p.CreatedAt) > 30*time.Minute {
		delete(u.confirmAgents, token)
		return confirmAgentDelete{}, false
	}
	return p, true
}

func (u *Updater) agentStillOnline(ctx context.Context, a config.AgentConfig) bool {
	if a.Mode == "reverse" && u.reverse != nil {
		if u.reverse.IsOnline(a.ID) {
			return true
		}
		_, err := u.reverse.Snapshot(ctx, a)
		return err == nil
	}
	if u.agents != nil {
		_, _, err := u.agents.Snapshot(ctx, a.ID)
		return err == nil
	}
	return false
}

func (u *Updater) removeAgentRecord(ctx context.Context, cb telegram.Callback, token string, a config.AgentConfig, title string) {
	if u.store == nil {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "当前配置不支持删除")
		return
	}
	if err := u.store.RemoveServer(a.ID); err != nil {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "删除失败")
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, friendlyErrorText("❌ 删除失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
		return
	}
	u.mu.Lock()
	delete(u.confirmAgents, token)
	u.mu.Unlock()
	u.refreshAgents()
	_ = u.bot.AnswerCallback(ctx, cb.ID, "已删除")
	_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, fmt.Sprintf("%s：%s", title, a.Name), telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
}

func (u *Updater) showRemoteProject(ctx context.Context, messageID int64, raw string) {
	agentID, key, ok := splitRemoteKey(raw)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程项目参数无效", navKeyboard())
		return
	}
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程 VPS 不存在", navKeyboard())
		return
	}
	var snap agent.Snapshot
	var err error
	agentCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if agentCfg.Mode == "reverse" && u.reverse != nil {
		snap, err = u.reverse.Snapshot(agentCtx, agentCfg)
	} else {
		_, snap, err = u.agents.Snapshot(agentCtx, agentID)
	}
	if err != nil {
		rows := [][]telegram.Button{
			{{Text: "🗑 删除服务器", Data: "adelask:" + agentID}},
			{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}},
		}
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 获取远程 VPS 失败", err), telegram.Keyboard(rows))
		return
	}
	for _, p := range snap.Projects {
		if p.Key == key {
			text := formatRemoteProjectStatus(snap.Name, p)
			rows := [][]telegram.Button{
				{{Text: "检查更新", Data: "rcheck:" + agentID + ":" + key}, {Text: "重启", Data: "rprestart:" + agentID + ":" + key}},
				{{Text: "启动", Data: "rpstart:" + agentID + ":" + key}, {Text: "停止", Data: "rpstop:" + agentID + ":" + key}},
				{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}},
				{{Text: "返回主界面", Data: "main"}},
			}
			_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
			return
		}
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程项目不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}}}))
}

func formatRemoteProjectStatus(agentName string, p agent.ProjectSnapshot) string {
	running := 0
	healthy := 0
	for _, d := range p.Containers {
		if d.State == "running" {
			running++
		}
		if d.Health == "healthy" {
			healthy++
		}
	}
	headStatus := "🔴 未运行"
	if running == len(p.Containers) && running > 0 {
		headStatus = "🟢 运行中"
	} else if running > 0 {
		headStatus = "🟡 部分运行"
	}
	if healthy > 0 && healthy == running {
		headStatus += " · 健康"
	}
	lines := []string{fmt.Sprintf("🌐 %s", agentName), fmt.Sprintf("🐳 %s", p.Name), headStatus, "", "📌 概览", fmt.Sprintf("🏷️ 类型：%s", typeLabel(p.Type)), fmt.Sprintf("📦 容器：%d 个 · 运行中 %d 个", len(p.Containers), running)}
	if p.WorkingDir != "" {
		lines = append(lines, "📁 目录："+p.WorkingDir)
	}
	if p.ConfigFile != "" {
		lines = append(lines, "🧩 Compose："+p.ConfigFile)
	}
	lines = append(lines, "", "📦 容器详情")
	for _, d := range p.Containers {
		lines = append(lines, formatContainerDetail(d)...)
	}
	return strings.Join(lines, "\n")
}

func (u *Updater) handleRemoteProjectAction(ctx context.Context, cb telegram.Callback) {
	parts := strings.SplitN(cb.Data, ":", 2)
	if len(parts) != 2 {
		return
	}
	actionName, raw := parts[0], parts[1]
	agentID, key, ok := splitRemoteKey(raw)
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "参数无效")
		return
	}
	action := ""
	switch actionName {
	case "rpstart":
		action = "start"
	case "rpstop":
		action = "stop"
	case "rprestart":
		action = "restart"
	}
	if action == "" {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "操作无效")
		return
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "执行中")
	_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "⏳ 正在执行远程操作…", nil)
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "服务器不存在")
		return
	}
	var err error
	actionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if agentCfg.Mode == "reverse" && u.reverse != nil {
		err = u.reverse.ProjectAction(actionCtx, agentCfg, key, action)
	} else {
		err = u.agents.ProjectAction(actionCtx, agentID, key, action)
	}
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, friendlyErrorText("❌ 远程操作失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回项目", Data: "rproject:" + agentID + ":" + key}, {Text: "返回远程列表", Data: "agents"}}}))
		return
	}
	u.showRemoteProject(ctx, cb.MessageID, agentID+":"+key)
}

func splitRemoteKey(raw string) (agentID, key string, ok bool) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (u *Updater) showProject(ctx context.Context, messageID int64, key string) {
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "⏳ 正在读取本机 Docker 项目…", nil)
	p, err := u.docker.Project(ctx, key)
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 项目不存在", navKeyboard())
		return
	}
	text := u.formatProjectStatus(ctx, p)
	rows := [][]telegram.Button{
		{{Text: "检查更新", Data: "pcheck:" + p.Key}, {Text: "重启", Data: "prestart:" + p.Key}},
		{{Text: "启动", Data: "pstart:" + p.Key}, {Text: "停止", Data: "pstop:" + p.Key}},
		{{Text: "删除", Data: "pdelete:" + p.Key}},
		{{Text: "返回列表", Data: "home"}, {Text: "返回主界面", Data: "main"}},
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
}

func (u *Updater) formatProjectStatus(ctx context.Context, p dockerx.ProjectInfo) string {
	details := []dockerx.ContainerDetail{}
	running := 0
	healthy := 0
	for _, c := range p.Containers {
		d, err := u.docker.ContainerDetail(ctx, c.ID)
		if err != nil {
			continue
		}
		details = append(details, d)
		if d.State == "running" {
			running++
		}
		if d.Health == "healthy" {
			healthy++
		}
	}

	headStatus := "🔴 未运行"
	if running == len(p.Containers) && running > 0 {
		headStatus = "🟢 运行中"
	} else if running > 0 {
		headStatus = "🟡 部分运行"
	}
	if healthy > 0 && healthy == running {
		headStatus += " · 健康"
	}

	lines := []string{
		fmt.Sprintf("🐳 %s", p.Name),
		headStatus,
		"",
		"📌 概览",
		fmt.Sprintf("🏷️ 类型：%s", typeLabel(p.Type)),
		fmt.Sprintf("📦 容器：%d 个 · 运行中 %d 个", len(p.Containers), running),
	}
	if p.WorkingDir != "" {
		lines = append(lines, "📁 目录："+p.WorkingDir)
	}
	if p.ConfigFile != "" {
		lines = append(lines, "🧩 Compose："+p.ConfigFile)
	}
	lines = append(lines, "")

	if len(details) == 0 {
		lines = append(lines, "⚠️ 容器详情读取失败。")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "📦 容器详情")
	for _, d := range details {
		lines = append(lines, formatContainerDetail(d)...)
	}
	return strings.Join(lines, "\n")
}

func formatContainerDetail(d dockerx.ContainerDetail) []string {
	state := stateLabel(d.State)
	if d.Health != "" {
		state += " · " + healthLabel(d.Health)
	}
	name := d.Info.Name
	if d.Service != "" && d.Service != d.Info.Name {
		name += " / " + d.Service
	}

	lines := []string{
		fmt.Sprintf("%s %s", stateIcon(d.State), name),
		fmt.Sprintf("🚦 状态：%s", state),
		fmt.Sprintf("🖼️ 镜像：%s", d.Info.Image),
	}
	if v := d.Version.Display(); v != "" && v != "-" {
		lines = append(lines, "🏷️ 版本："+v)
	}
	if d.State == "running" {
		lines = append(lines, fmt.Sprintf("📊 占用：CPU %.2f%% · 内存 %s", d.CPUPercent, dockerx.FormatBytes(d.Memory)))
	}
	if d.Ports != "-" {
		lines = append(lines, "🌐 端口："+d.Ports)
	}
	if d.State == "running" && d.NetRx+d.NetTx > 0 {
		lines = append(lines, fmt.Sprintf("📡 网络：↓%s ↑%s", dockerx.FormatBytes(d.NetRx), dockerx.FormatBytes(d.NetTx)))
	}
	if d.State == "running" && d.BlockRead+d.BlockWrite > 0 {
		lines = append(lines, fmt.Sprintf("💾 磁盘：读 %s · 写 %s", dockerx.FormatBytes(d.BlockRead), dockerx.FormatBytes(d.BlockWrite)))
	}
	meta := []string{fmt.Sprintf("🆔 ID：%s", shortID(d.Info.ID))}
	if d.Restarts > 0 {
		meta = append(meta, fmt.Sprintf("🔁 重启：%d", d.Restarts))
	}
	if d.Started != "-" {
		meta = append(meta, "⏱️ 启动："+d.Started)
	}
	lines = append(lines, strings.Join(meta, " · "))
	lines = append(lines, "")
	return lines
}

func (u *Updater) manualRemoteAgentCheck(ctx context.Context, messageID int64, agentID string) {
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程 VPS 不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
		return
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "⏳ 正在检查远程 VPS 更新："+agentCfg.Name, nil)
	updates, err := u.remoteAgentUpdates(ctx, agentCfg)
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 检查远程 VPS 失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}}}))
		return
	}
	if len(updates) == 0 {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "✅ 没有发现更新\n\n"+agentCfg.Name+" 当前已是最新。", telegram.Keyboard([][]telegram.Button{{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}}, {{Text: "返回主界面", Data: "main"}}}))
		return
	}
	u.showRemoteUpdates(ctx, messageID, agentCfg, "🌐 远程 VPS 发现更新\n\n服务器："+agentCfg.Name+"\n\n", updates, "agent:"+agentID)
}

func (u *Updater) manualRemoteProjectCheck(ctx context.Context, messageID int64, raw string) {
	agentID, key, ok := splitRemoteKey(raw)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程项目参数无效", navKeyboard())
		return
	}
	agentCfg, ok := u.findAgent(agentID)
	if !ok {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程 VPS 不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回远程列表", Data: "agents"}, {Text: "返回主界面", Data: "main"}}}))
		return
	}
	var snap agent.Snapshot
	var err error
	if agentCfg.Mode == "reverse" && u.reverse != nil {
		snap, err = u.reverse.Snapshot(ctx, agentCfg)
	} else {
		_, snap, err = u.agents.Snapshot(ctx, agentID)
	}
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 获取远程项目失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}}}))
		return
	}
	var project *agent.ProjectSnapshot
	for i := range snap.Projects {
		if snap.Projects[i].Key == key {
			project = &snap.Projects[i]
			break
		}
	}
	if project == nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 远程项目不存在", telegram.Keyboard([][]telegram.Button{{{Text: "返回该 VPS", Data: "agent:" + agentID}, {Text: "返回远程列表", Data: "agents"}}}))
		return
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "⏳ 正在检查远程项目更新："+project.Name, nil)
	all, err := u.remoteAgentUpdates(ctx, agentCfg)
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, friendlyErrorText("❌ 检查远程项目失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回项目", Data: "rproject:" + agentID + ":" + key}, {Text: "返回远程列表", Data: "agents"}}}))
		return
	}
	ids := map[string]bool{}
	for _, c := range project.Containers {
		ids[c.Info.ID] = true
	}
	updates := []agent.UpdateInfo{}
	for _, up := range all {
		if ids[up.Container.ID] {
			updates = append(updates, up)
		}
	}
	if len(updates) == 0 {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "✅ 没有发现更新\n\n"+project.Name+" 当前已是最新。", telegram.Keyboard([][]telegram.Button{{{Text: "返回项目", Data: "rproject:" + agentID + ":" + key}, {Text: "返回该 VPS", Data: "agent:" + agentID}}, {{Text: "返回主界面", Data: "main"}}}))
		return
	}
	u.showRemoteUpdates(ctx, messageID, agentCfg, "🌐 远程项目发现更新\n\n服务器："+agentCfg.Name+"\n项目："+project.Name+"\n\n", updates, "rproject:"+agentID+":"+key)
}

func (u *Updater) remoteAgentUpdates(ctx context.Context, agentCfg config.AgentConfig) ([]agent.UpdateInfo, error) {
	if agentCfg.Mode == "reverse" && u.reverse != nil {
		return u.reverse.CheckUpdates(ctx, agentCfg)
	}
	_, updates, err := u.agents.CheckUpdates(ctx, agentCfg.ID)
	return updates, err
}

func (u *Updater) showRemoteUpdates(ctx context.Context, messageID int64, agentCfg config.AgentConfig, text string, updates []agent.UpdateInfo, backData string) {
	rows := [][]telegram.Button{}
	u.mu.Lock()
	for _, up := range updates {
		token := randomToken()
		u.manualChecks[token] = pendingUpdate{Token: token, Container: up.Container, OldVersion: up.OldVersion, NewVersion: up.NewVersion, AgentID: agentCfg.ID, AgentName: agentCfg.Name, MessageID: messageID, CreatedAt: time.Now()}
		text += fmt.Sprintf("• %s：%s → %s\n", up.Container.Name, up.OldVersion.Display(), up.NewVersion.Display())
		rows = append(rows, []telegram.Button{{Text: "更新 " + up.Container.Name, Data: "pupdate:" + token}})
	}
	u.mu.Unlock()
	rows = append(rows, []telegram.Button{{Text: "返回", Data: backData}, {Text: "返回远程列表", Data: "agents"}})
	rows = append(rows, []telegram.Button{{Text: "返回主界面", Data: "main"}})
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
}

func (u *Updater) manualProjectCheck(ctx context.Context, messageID int64, key string) {
	p, err := u.docker.Project(ctx, key)
	if err != nil {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "❌ 项目不存在", navKeyboard())
		return
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "⏳ 正在检查更新："+p.Name, nil)
	updates := []pendingUpdate{}
	for _, c := range p.Containers {
		oldV, newV, err := u.checkContainerVersions(ctx, c)
		if err != nil {
			u.log.Warn("manual check failed", "container", c.Name, "error", err)
			continue
		}
		if !dockerx.SameEffectiveImage(oldV, newV) {
			updates = append(updates, pendingUpdate{Token: randomToken(), Container: c, OldVersion: oldV, NewVersion: newV, MessageID: messageID, CreatedAt: time.Now()})
		}
	}
	if len(updates) == 0 {
		_ = u.bot.EditMessageWithKeyboard(ctx, messageID, "✅ 没有发现更新\n\n"+p.Name+" 当前已是最新。", telegram.Keyboard([][]telegram.Button{{{Text: "返回项目", Data: "project:" + p.Key}, {Text: "返回列表", Data: "home"}}, {{Text: "返回主界面", Data: "main"}}}))
		return
	}
	u.mu.Lock()
	for _, up := range updates {
		u.manualChecks[up.Token] = up
	}
	u.mu.Unlock()
	text := "🐳 发现更新\n\n项目：" + p.Name + "\n\n"
	rows := [][]telegram.Button{}
	for _, up := range updates {
		text += fmt.Sprintf("• %s：%s → %s\n", up.Container.Name, up.OldVersion.Display(), up.NewVersion.Display())
		rows = append(rows, []telegram.Button{{Text: "更新 " + up.Container.Name, Data: "pupdate:" + up.Token}})
	}
	rows = append(rows, []telegram.Button{{Text: "返回项目", Data: "project:" + p.Key}, {Text: "返回列表", Data: "home"}})
	rows = append(rows, []telegram.Button{{Text: "返回主界面", Data: "main"}})
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
}

func (u *Updater) checkContainerVersions(ctx context.Context, c dockerx.ContainerInfo) (dockerx.ImageVersion, dockerx.ImageVersion, error) {
	oldVersion, err := u.docker.InspectImageVersionByIDWithRef(ctx, c.ImageID, c.Image)
	if err != nil {
		return oldVersion, dockerx.ImageVersion{}, err
	}
	if oldVersion.ID == "" {
		oldVersion.ID = c.ImageID
	}
	newVersion, err := u.docker.RemoteImageVersion(ctx, c.Image)
	return oldVersion, newVersion, err
}

func (u *Updater) handleBatchUpdate(ctx context.Context, cb telegram.Callback, token string) {
	u.mu.Lock()
	tokens, ok := u.batchUpdates[token]
	if ok {
		delete(u.batchUpdates, token)
	}
	updates := []pendingUpdate{}
	for _, t := range tokens {
		if p, exists := u.pending[t]; exists {
			delete(u.pending, t)
			p.MessageID = cb.MessageID
			updates = append(updates, p)
			continue
		}
		if p, exists := u.manualChecks[t]; exists {
			delete(u.manualChecks, t)
			p.MessageID = cb.MessageID
			updates = append(updates, p)
		}
	}
	u.mu.Unlock()
	if !ok || len(updates) == 0 {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "这些更新已经处理或过期")
		return
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "开始全部更新")
	_ = u.bot.EditMessage(ctx, cb.MessageID, fmt.Sprintf("⏳ 正在更新 %d 个容器…", len(updates)))
	go func() {
		success := 0
		failed := []string{}
		for i, p := range updates {
			_ = u.bot.EditMessage(ctx, cb.MessageID, fmt.Sprintf("⏳ 正在更新 %d/%d：%s", i+1, len(updates), p.Container.Name))
			if err := u.applyUpdateOnce(ctx, p); err != nil {
				failed = append(failed, p.Container.Name+"："+err.Error())
				continue
			}
			success++
		}
		lines := []string{fmt.Sprintf("✅ 批量更新完成：成功 %d / %d", success, len(updates))}
		if len(failed) > 0 {
			lines = append(lines, "", "失败：")
			for _, item := range failed {
				lines = append(lines, "• "+item)
			}
		}
		_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, strings.Join(lines, "\n"), navKeyboard())
	}()
}

func (u *Updater) handleManualUpdate(ctx context.Context, cb telegram.Callback, token string) {
	u.mu.Lock()
	p, ok := u.manualChecks[token]
	if ok {
		delete(u.manualChecks, token)
	}
	u.mu.Unlock()
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "这个更新已经处理或过期")
		return
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "开始更新")
	_ = u.bot.EditMessage(ctx, cb.MessageID, formatUpdating(p))
	go u.applyUpdate(context.Background(), p)
}

func (u *Updater) handleProjectAction(ctx context.Context, cb telegram.Callback) {
	parts := strings.SplitN(cb.Data, ":", 2)
	if len(parts) != 2 {
		return
	}
	action, key := parts[0], parts[1]
	p, err := u.docker.Project(ctx, key)
	if err != nil {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "项目不存在")
		return
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "执行中")
	for _, c := range p.Containers {
		switch action {
		case "pstart":
			err = u.docker.StartContainer(ctx, c.ID)
		case "pstop":
			err = u.docker.StopContainer(ctx, c.ID)
		case "prestart":
			err = u.docker.RestartContainer(ctx, c.ID)
		}
		if err != nil {
			_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, friendlyErrorText("❌ 操作失败", err), telegram.Keyboard([][]telegram.Button{{{Text: "返回项目", Data: "project:" + p.Key}, {Text: "返回列表", Data: "home"}}, {{Text: "返回主界面", Data: "main"}}}))
			return
		}
	}
	u.showProject(ctx, cb.MessageID, p.Key)
}

func (u *Updater) confirmProjectDelete(ctx context.Context, cb telegram.Callback, key string) {
	p, err := u.docker.Project(ctx, key)
	if err != nil {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "项目不存在")
		return
	}
	token := randomToken()
	u.mu.Lock()
	u.confirmDeletes[token] = confirmDelete{Token: token, Project: p, MessageID: cb.MessageID, CreatedAt: time.Now()}
	u.mu.Unlock()
	_ = u.bot.AnswerCallback(ctx, cb.ID, "需要二次确认")
	text := fmt.Sprintf("⚠️ 确认删除 Docker 项目？\n\n项目：%s\n类型：%s\n容器数：%d\n\n这会强制删除容器，但不会删除 volume。", p.Name, typeLabel(p.Type), len(p.Containers))
	rows := [][]telegram.Button{{{Text: "确认删除", Data: "delok:" + token}}, {{Text: "取消", Data: "project:" + p.Key}}, {{Text: "返回主界面", Data: "main"}}}
	_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, text, telegram.Keyboard(rows))
}

func (u *Updater) executeProjectDelete(ctx context.Context, cb telegram.Callback, token string) {
	u.mu.Lock()
	p, ok := u.confirmDeletes[token]
	if ok {
		delete(u.confirmDeletes, token)
	}
	u.mu.Unlock()
	if !ok {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "删除确认已过期")
		return
	}
	_ = u.bot.AnswerCallback(ctx, cb.ID, "正在删除")
	for _, c := range p.Project.Containers {
		if err := u.docker.DeleteContainer(ctx, c.ID); err != nil {
			_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, friendlyErrorText("❌ 删除失败", err), navKeyboard())
			return
		}
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, cb.MessageID, "✅ 已删除项目："+p.Project.Name, navKeyboard())
}

func (u *Updater) showSettings(ctx context.Context, messageID int64) {
	text := fmt.Sprintf("⚙️ 自动检查间隔\n\n当前：%s\n\n选择新的自动检查间隔。", u.interval().String())
	rows := [][]telegram.Button{
		{{Text: "30 分钟", Data: "interval:30m"}, {Text: "1 小时", Data: "interval:1h"}},
		{{Text: "6 小时", Data: "interval:6h"}, {Text: "12 小时", Data: "interval:12h"}},
		{{Text: "24 小时", Data: "interval:24h"}},
		{{Text: "返回列表", Data: "home"}, {Text: "返回主界面", Data: "main"}},
	}
	_ = u.bot.EditMessageWithKeyboard(ctx, messageID, text, telegram.Keyboard(rows))
}

func (u *Updater) handleSetInterval(ctx context.Context, cb telegram.Callback, raw string) {
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		_ = u.bot.AnswerCallback(ctx, cb.ID, "间隔无效")
		return
	}
	u.mu.Lock()
	u.currentInterval = d
	u.mu.Unlock()
	_ = u.bot.AnswerCallback(ctx, cb.ID, "已设置为 "+d.String())
	u.showSettings(ctx, cb.MessageID)
}

func (u *Updater) interval() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.currentInterval <= 0 {
		return u.cfg.CheckInterval
	}
	return u.currentInterval
}

func navKeyboard() map[string]any {
	return telegram.Keyboard([][]telegram.Button{{{Text: "返回列表", Data: "home"}, {Text: "返回主界面", Data: "main"}}})
}

func homeKeyboard() map[string]any { return navKeyboard() }

func typeLabel(t string) string {
	if t == "compose" {
		return "📦 Compose"
	}
	return "🐳 Docker"
}

func stateIcon(s string) string {
	switch s {
	case "running":
		return "🟢"
	case "exited", "dead":
		return "🔴"
	case "paused":
		return "🟡"
	default:
		return "⚪️"
	}
}

func stateLabel(s string) string {
	switch s {
	case "running":
		return "🟢 运行中"
	case "exited":
		return "🔴 已停止"
	case "paused":
		return "🟡 已暂停"
	case "restarting":
		return "🟡 重启中"
	case "dead":
		return "🔴 dead"
	default:
		return s
	}
}

func healthLabel(s string) string {
	switch s {
	case "healthy":
		return "健康"
	case "unhealthy":
		return "不健康"
	case "starting":
		return "健康检查中"
	default:
		return s
	}
}
