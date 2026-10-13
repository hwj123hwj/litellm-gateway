// Package console implements Gateway's desktop UI entirely with MyGo native controls.
package console

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/connection"
)

type App struct {
	Version                                                               string
	store                                                                 *connection.Store
	profiles                                                              connection.State
	page                                                                  string
	dispatch                                                              func(func())
	sequence                                                              uint64
	cancel                                                                context.CancelFunc
	busy, notice, problem                                                 string
	loaded                                                                bool
	dashboard                                                             Dashboard
	providers                                                             []Provider
	channels                                                              []AccountChannel
	channelLogin                                                          *ChannelLogin
	channelClient                                                         *connection.Client
	channelList                                                           ui.ListState
	channelSetup                                                          string
	OpenLoginURL                                                          func(string) error
	CopyText                                                              func(string)
	models                                                                []Model
	routes                                                                []Route
	logs                                                                  []Log
	logLimit, logFilter                                                   string
	logSelected                                                           int
	table                                                                 ui.ListState
	listViews                                                             listViewState
	modelFilter, modelCategory                                            string
	skills                                                                Skills
	project, loadedProject, targets                                       string
	skillSelection                                                        map[string]bool
	skillDirty                                                            bool
	skillFilter, skillTag                                                 string
	skillScopeOpen                                                        bool
	skillExpanded                                                         map[string]bool
	skillSource                                                           string
	market                                                                MarketPage
	marketQuery, marketSort, marketSection                                string
	marketSections                                                        []MarketSection
	marketPage                                                            int
	marketInstalled                                                       []MarketInstalled
	marketExpanded                                                        map[string]bool
	modelPickerOpen                                                       bool
	modelPickerFilter                                                     string
	modelPickerList                                                       ui.ListState
	memories                                                              []Memory
	memoryFilter                                                          string
	memoryScope, memoryKey, memoryText                                    string
	prompt                                                                Prompt
	promptDraft                                                           string
	configs                                                               map[string]ClientConfig
	configSelection                                                       map[string]map[string]bool
	feedback                                                              []Feedback
	chatInput, chatReply, chatUser, chatStatus, feedbackNote              string
	connSelect                                                            string
	chatModels                                                            []string
	chatModelOptions                                                      map[string]ChatModelOption
	chatModel, chatDefault                                                string
	chatList                                                              ui.ScrollState
	editorOpen                                                            bool
	editorKind, editorTitle, editorID, editorName, editorURL, editorToken string
	editorOrder                                                           []string
	editorCaps                                                            map[string]bool
	editorModalities                                                      string
	deleteOpen                                                            bool
	deleteAction                                                          func()
	browserOpen                                                           bool
	browserKind                                                           string
	PickFolder                                                            func(done func(path string, ok bool))
	browserPath, browserParent                                            string
	browserDirs                                                           []string
}

func New(store *connection.Store) *App {
	a := &App{store: store, profiles: store.State(), page: "connections", logLimit: "50", logSelected: -1,
		memoryScope: "global", memoryFilter: "all", skillSelection: map[string]bool{}, skillTag: skillsTagAll,
		modelCategory: modelCategoryAll, marketSection: marketSectionAll,
		skillExpanded: map[string]bool{}, skillSource: skillSourceRepo, marketSort: "推荐", marketPage: 1,
		marketExpanded: map[string]bool{}, configs: map[string]ClientConfig{}, configSelection: map[string]map[string]bool{}}
	if a.profiles.ActiveID != "" {
		a.page = "overview"
	}
	return a
}
func (a *App) SetDispatcher(fn func(func())) { a.dispatch = fn }
func (a *App) Close()                        { a.stop(); a.dispatch = nil }
func (a *App) Start() {
	a.applyPrefs()
	if a.page != "connections" {
		a.reload()
	}
}

// applyPrefs 应用本地 UI 偏好；非法值回退默认。
func (a *App) applyPrefs() {
	prefs := a.store.LoadPrefs()
	if prefs.SkillSource == skillSourceRepo || prefs.SkillSource == skillSourceMarket {
		a.skillSource = prefs.SkillSource
	}
	for _, category := range modelCategories {
		if prefs.ModelCategory == category {
			a.modelCategory = prefs.ModelCategory
		}
	}
	if prefs.MarketSection != "" {
		a.marketSection = prefs.MarketSection
	}
	if prefs.ChatModel != "" {
		a.chatModel = prefs.ChatModel
	}
}

// savePrefs 把需要记住的 UI 状态写本地（尽力而为）。
func (a *App) savePrefs() {
	a.store.SavePrefs(connection.Prefs{
		ChatModel:     a.chatModel,
		SkillSource:   a.skillSource,
		ModelCategory: a.modelCategory,
		MarketSection: a.marketSection,
	})
}
func (a *App) ShowConnections() { a.navigate("connections") }
func (a *App) stop() {
	a.cancelChannelSession()
	a.sequence++
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.busy == "助理回复中" {
		a.chatStatus = "已停止"
	}
	a.busy = ""
}
func (a *App) active() connection.Profile {
	for _, p := range a.profiles.Profiles {
		if p.ID == a.profiles.ActiveID {
			return p
		}
	}
	return connection.Profile{}
}
func (a *App) navigate(page string) {
	if a.skillDirty && page != "skills" {
		a.problem = "技能选择尚未保存，请先保存或撤销。"
		return
	}
	if page == a.page {
		return
	}
	a.stop()
	a.page = page
	a.problem = ""
	a.notice = ""
	a.loaded = false
	a.table = ui.ListState{}
	a.logSelected = -1
	if page != "connections" {
		a.reload()
	}
}

// Work captures inputs on the UI thread; only the completion mutates UI state.
// Navigation, host switches and closing invalidate/cancel outstanding results.
func (a *App) work(label string, timeout time.Duration, task func(context.Context, *connection.Client) (func(), error)) {
	if a.busy != "" || a.dispatch == nil {
		return
	}
	client, err := a.store.Client()
	if err != nil {
		a.problem = err.Error()
		return
	}
	a.launch(label, timeout, func(ctx context.Context) (func(), error) { return task(ctx, client) })
}
func (a *App) launch(label string, timeout time.Duration, task func(context.Context) (func(), error)) {
	if a.busy != "" || a.dispatch == nil {
		return
	}
	a.sequence++
	seq := a.sequence
	dispatch := a.dispatch
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	a.cancel = cancel
	a.busy = label
	a.problem = ""
	a.notice = ""
	go func() {
		apply, err := task(ctx)
		cancel()
		dispatch(func() {
			if seq != a.sequence {
				return
			}
			a.cancel = nil
			a.busy = ""
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.problem = err.Error()
					if label == "助理回复中" {
						a.chatStatus = "回复中断"
					}
				}
				return
			}
			if apply != nil {
				apply()
			}
		})
	}()
}
func (a *App) reload() {
	page, project, filter, limit := a.page, a.loadedProject, a.memoryFilter, a.logLimit
	source, marketQuery, marketSort, marketSection, marketPage := a.skillSource, a.marketQuery, a.marketSort, a.marketSection, a.marketPage
	a.work("正在读取", 35*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		switch page {
		case "overview":
			var d Dashboard
			err := c.JSON(ctx, "GET", "/dashboard", nil, &d)
			var channels struct {
				Channels []AccountChannel `json:"channels"`
			}
			// 渠道清单用于过期提醒；拉取失败不影响概览指标。
			channelsErr := c.JSON(ctx, "GET", "/channels", nil, &channels)
			return func() {
				// 后台直接写渠道状态会与原生渲染竞态，并绕过主机切换的序列校验。
				if channelsErr == nil {
					a.channels = channels.Channels
				}
				a.dashboard = d
				a.loaded = true
			}, err
		case "models":
			var m struct {
				Models []Model `json:"models"`
			}
			var r struct {
				Routes []Route `json:"routes"`
			}
			if err := c.JSON(ctx, "GET", "/models", nil, &m); err != nil {
				return nil, err
			}
			err := c.JSON(ctx, "GET", "/routes", nil, &r)
			return func() { a.models = m.Models; a.routes = r.Routes; a.loaded = true }, err
		case "channels":
			var d struct {
				Channels []AccountChannel `json:"channels"`
			}
			err := c.JSON(ctx, "GET", "/channels", nil, &d)
			return func() { a.channels = orderChannels(d.Channels); a.loaded = true }, err
		case "providers":
			var d struct {
				Providers []Provider `json:"providers"`
			}
			err := c.JSON(ctx, "GET", "/providers", nil, &d)
			return func() { a.providers = d.Providers; a.loaded = true }, err
		case "logs":
			var d struct {
				Logs []Log `json:"logs"`
			}
			err := c.JSON(ctx, "GET", "/logs?limit="+url.QueryEscape(limit), nil, &d)
			return func() { a.logs = d.Logs; a.logSelected = -1; a.loaded = true }, err
		case "skills":
			if source == skillSourceMarket {
				// 分类列表尽力而为：拉取失败只影响筛选下拉，不影响浏览。
				var sections struct {
					Sections []MarketSection `json:"sections"`
				}
				if err2 := c.JSON(ctx, "GET", "/skills/market/sections", nil, &sections); err2 != nil {
					sections.Sections = nil
				}
				sectionID := 0
				for _, s := range sections.Sections {
					if s.Name == marketSection {
						sectionID = s.ID
					}
				}
				endpoint := fmt.Sprintf("/skills/market/browse?q=%s&sort=%s&page=%d",
					url.QueryEscape(marketQuery), url.QueryEscape(marketSortParam(marketSort)), marketPage)
				if sectionID > 0 {
					endpoint += fmt.Sprintf("&section=%d", sectionID)
				}
				var browse MarketPage
				err := c.JSON(ctx, "GET", endpoint, nil, &browse)
				if err == nil && browse.Error != "" {
					err = errors.New(browse.Error)
				}
				var local struct {
					Installed []MarketInstalled `json:"installed"`
				}
				if err == nil {
					if err2 := c.JSON(ctx, "GET", "/skills/market/installed", nil, &local); err2 != nil {
						err = err2
					}
				}
				return func() {
					a.market = browse
					a.marketSections = sections.Sections
					a.marketInstalled = local.Installed
					if a.marketSection != marketSectionAll && len(sections.Sections) > 0 {
						found := false
						for _, s := range sections.Sections {
							if s.Name == a.marketSection {
								found = true
							}
						}
						if !found {
							a.marketSection = marketSectionAll
						}
					}
					a.loaded = true
				}, err
			}
			var d Skills
			err := c.JSON(ctx, "GET", "/skills"+skillsQuery(project), nil, &d)
			if err == nil && d.Error != "" {
				err = errors.New(d.Error)
			}
			return func() { a.acceptSkills(d); a.loaded = true }, err
		case "memory":
			endpoint := "/memories?limit=200"
			if filter != "all" {
				endpoint += "&status=" + url.QueryEscape(filter)
			}
			var d struct {
				Memories []Memory `json:"memories"`
			}
			err := c.JSON(ctx, "GET", endpoint, nil, &d)
			return func() { a.memories = d.Memories; a.loaded = true }, err
		case "settings":
			var p Prompt
			if err := c.JSON(ctx, "GET", "/assistant/prompt", nil, &p); err != nil {
				return nil, err
			}
			configs := map[string]ClientConfig{}
			for _, target := range []string{"pi", "zcode", "harness"} {
				var config ClientConfig
				if err := c.JSON(ctx, "GET", "/"+target, nil, &config); err != nil {
					config.Error = err.Error()
				}
				configs[target] = config
			}
			var f struct {
				Feedback []Feedback `json:"feedback"`
			}
			err := c.JSON(ctx, "GET", "/assistant/feedback?limit=20", nil, &f)
			return func() {
				a.prompt = p
				a.promptDraft = p.Custom
				a.configs = configs
				a.initConfigSelection()
				a.feedback = f.Feedback
				a.loaded = true
			}, err
		case "assistant":
			var m struct {
				Default string            `json:"default"`
				Models  []string          `json:"models"`
				Options []ChatModelOption `json:"model_options"`
			}
			err := c.JSON(ctx, "GET", "/assistant/models", nil, &m)
			return func() {
				a.chatModels = m.Models
				a.chatModelOptions = map[string]ChatModelOption{}
				for _, option := range m.Options {
					a.chatModelOptions[option.ID] = option
				}
				a.chatDefault = m.Default
				if a.chatModel != chatDefaultOption {
					found := false
					for _, id := range m.Models {
						if id == a.chatModel {
							found = true
						}
					}
					if !found {
						a.chatModel = chatDefaultOption
					}
				}
				a.loaded = true
			}, err
		}
		return nil, nil
	})
}
func (a *App) mutate(label, method, path string, body any) {
	a.work(label, 60*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		err := c.JSON(ctx, method, path, body, nil)
		return func() { a.reload(); a.notice = "已完成" }, err
	})
}
func (a *App) selectConnection(id string) {
	st, err := a.store.Select(id)
	if err != nil {
		a.problem = err.Error()
		return
	}
	a.stop()
	a.profiles = st
	a.resetHost()
	a.page = "overview"
	a.connSelect = a.active().Name
	a.reload()
}

const chatDefaultOption = "网关默认"

// localConnection 判断当前连接的网关是否跑在本机（回环地址）——只有
// 这种情况下原生目录面板选择的路径才属于网关主机。
func (a *App) localConnection() bool {
	u, err := url.Parse(strings.TrimSpace(a.active().URL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// pickLocalFolder 用原生面板（sheet 挂主窗口）选择本机目录；取消或未
// 注入面板能力（无 cgo 构建）时保持原值不变。
func (a *App) pickLocalFolder() {
	if a.PickFolder == nil {
		return
	}
	a.PickFolder(func(path string, ok bool) {
		if !ok {
			return
		}
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			a.dispatch(func() { a.project = trimmed })
		}
	})
}

// routeCandidates 返回模型配置过、但尚未加入当前回退链的 Provider。
func (a *App) routeCandidates(model string, order []string) []string {
	inChain := map[string]bool{}
	for _, name := range order {
		inChain[name] = true
	}
	candidates := []string{}
	seen := map[string]bool{}
	for _, r := range a.routes {
		if r.Model != model {
			continue
		}
		for _, p := range r.Providers {
			if !inChain[p.Name] && !seen[p.Name] {
				seen[p.Name] = true
				candidates = append(candidates, p.Name)
			}
		}
	}
	return candidates
}

// routeOrderFor 返回模型当前回退链顺序；无路由时回退到模型自带 Provider。
func (a *App) routeOrderFor(model string) []string {
	for _, r := range a.routes {
		if r.Model == model {
			names := make([]string, 0, len(r.Providers))
			for _, p := range r.Providers {
				names = append(names, p.Name)
			}
			return names
		}
	}
	for _, m := range a.models {
		if m.Name == model {
			return append([]string(nil), m.Providers...)
		}
	}
	return nil
}

func (a *App) chatModelParam() string {
	if a.chatModel == chatDefaultOption {
		return ""
	}
	return a.chatModel
}

func (a *App) resetHost() {
	a.listViews = listViewState{}
	a.loaded = false
	a.dashboard = Dashboard{}
	a.models = nil
	a.routes = nil
	a.providers = nil
	a.channels = nil
	a.channelSetup = ""
	a.channelList = ui.ListState{}
	a.logs = nil
	a.memories = nil
	a.skills = Skills{}
	a.skillDirty = false
	a.loadedProject = ""
	a.project = ""
	a.targets = ""
	a.skillSelection = map[string]bool{}
	a.skillFilter, a.skillTag = "", skillsTagAll
	a.skillScopeOpen = false
	a.skillExpanded = map[string]bool{}
	a.skillSource = skillSourceRepo
	a.market = MarketPage{}
	a.marketQuery, a.marketSort, a.marketSection = "", "推荐", marketSectionAll
	a.marketSections = nil
	a.marketPage = 1
	a.marketInstalled = nil
	a.marketExpanded = map[string]bool{}
	a.modelPickerOpen = false
	a.modelPickerFilter = ""
	a.modelPickerList = ui.ListState{}
	a.modelCategory = modelCategoryAll
	a.prompt = Prompt{}
	a.promptDraft = ""
	a.configs = map[string]ClientConfig{}
	a.configSelection = map[string]map[string]bool{}
	a.feedback = nil
	a.chatReply = ""
	a.chatInput = ""
	a.chatUser = ""
	a.chatStatus = ""
	a.chatModels = nil
	a.chatModelOptions = nil
	a.chatModel = chatDefaultOption
	a.chatDefault = ""
	a.feedbackNote = ""
	a.table = ui.ListState{}
	a.notice = ""
	a.problem = ""
	a.browserOpen = false
	a.browserPath, a.browserParent = "", ""
	a.browserDirs = nil
}
func skillsQuery(project string) string {
	if project == "" {
		return ""
	}
	return "?project=" + url.QueryEscape(project)
}

// marketSortParam 把市场排序选项的中文标签映射到商城 API 的 sort 值。
func marketSortParam(label string) string {
	switch label {
	case "最多安装":
		return "installs"
	case "名称":
		return "name"
	default:
		return "featured"
	}
}

// marketInstalledByName 按商城技能目录名查本机安装记录。
func (a *App) marketInstalledByName(name string) (MarketInstalled, bool) {
	for _, m := range a.marketInstalled {
		if m.Name == name {
			return m, true
		}
	}
	return MarketInstalled{}, false
}

// filteredChatModels 按搜索词筛助理模型：前缀匹配优先、其后包含，
// 均不分大小写；无过滤词时返回含“网关默认”的全量目录。
func (a *App) filteredChatModels() []string {
	all := append([]string{chatDefaultOption}, a.chatModels...)
	q := strings.ToLower(strings.TrimSpace(a.modelPickerFilter))
	if q == "" {
		return all
	}
	var starts, contains []string
	for _, m := range all {
		l := strings.ToLower(m)
		switch {
		case strings.HasPrefix(l, q):
			starts = append(starts, m)
		case strings.Contains(l, q) || strings.Contains(strings.ToLower(a.chatModelOptions[m].Provider), q) || strings.Contains(strings.ToLower(a.chatModelOptions[m].Channel), q):
			contains = append(contains, m)
		}
	}
	return append(starts, contains...)
}

// channelExpiryCounts 汇总渠道账号凭据的失效与临期数量（24 小时内到期）。
func (a *App) channelExpiryCounts() (expired, expiring int) {
	for _, ch := range a.channels {
		for _, account := range ch.Accounts {
			if account.Expired {
				expired++
				continue
			}
			if expiry, err := time.Parse(time.RFC3339, account.ExpiresAt); err == nil {
				if until := time.Until(expiry); until > 0 && until < 24*time.Hour {
					expiring++
				}
			}
		}
	}
	return expired, expiring
}

func split(value string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range strings.Split(value, ",") {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

// openBrowser 打开网关主机目录浏览器；起始路径用当前输入框的值，
// 留空时由网关返回其主目录。本机网关浏览的就是本机文件系统，
// 远程网关则与 Termius 的远程浏览同构——路径语义始终属于网关主机。
func (a *App) openBrowser() {
	a.openBrowserFor("project", a.project)
}
func (a *App) openBrowserFor(kind, start string) {
	a.browserKind = kind
	a.browserOpen = true
	a.browseHost(strings.TrimSpace(start))
}
func (a *App) browseHost(path string) {
	a.work("正在读取目录", 20*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		var d struct {
			Path   string   `json:"path"`
			Parent string   `json:"parent"`
			Dirs   []string `json:"dirs"`
		}
		endpoint := "/fs/dirs"
		if path != "" {
			endpoint += "?path=" + url.QueryEscape(path)
		}
		if err := c.JSON(ctx, "GET", endpoint, nil, &d); err != nil {
			return nil, err
		}
		return func() { a.browserPath, a.browserParent, a.browserDirs = d.Path, d.Parent, d.Dirs }, nil
	})
}
func (a *App) pickBrowserDir() {
	if a.browserPath == "" {
		return
	}
	a.applyPickedDir(a.browserPath)
	a.browserOpen = false
}
func (a *App) applyPickedDir(path string) {
	switch a.browserKind {
	case "memory":
		a.memoryKey = path
	default:
		a.project = path
	}
}

// clientSuggestions 汇总记忆范围键的可选客户端：已有 client 作用域
// 记忆里的键 + 已知客户端默认项。
func (a *App) clientSuggestions() []string {
	clients := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			clients = append(clients, v)
		}
	}
	for _, m := range a.memories {
		if m.Scope == "client" {
			add(m.ScopeKey)
		}
	}
	for _, c := range []string{"claude-code", "codex", "pi", "zcode", "harness"} {
		add(c)
	}
	return clients
}
func configInSync(cfg ClientConfig, choices map[string]bool) bool {
	if !cfg.Exists || len(cfg.MissingEntries) > 0 || len(cfg.Current) != len(sortedSelection(choices)) {
		return false
	}
	for _, id := range cfg.Current {
		if !choices[id] {
			return false
		}
	}
	return true
}
func (a *App) initConfigSelection() {
	a.configSelection = map[string]map[string]bool{}
	for target, cfg := range a.configs {
		choices := map[string]bool{}
		for _, m := range cfg.Desired {
			choices[m.ID] = !cfg.Exists
			for _, id := range cfg.Current {
				if id == m.ID {
					choices[m.ID] = true
				}
			}
		}
		a.configSelection[target] = choices
	}
}
func shortNumber(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
func percent(n *float64) string {
	if n == nil {
		return "未报告"
	}
	return fmt.Sprintf("%.1f%%", *n)
}
