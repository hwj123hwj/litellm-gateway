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
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/nativefolder"
)

type App struct {
	Version                                                                            string
	store                                                                              *connection.Store
	profiles                                                                           connection.State
	page                                                                               string
	dispatch                                                                           func(func())
	sequence                                                                           uint64
	cancel                                                                             context.CancelFunc
	busy, notice, problem                                                              string
	loaded                                                                             bool
	dashboard                                                                          Dashboard
	providers                                                                          []Provider
	models                                                                             []Model
	routes                                                                             []Route
	logs                                                                               []Log
	logLimit, logFilter                                                                string
	logSelected                                                                        int
	table                                                                              ui.ListState
	listViews                                                                          listViewState
	modelFilter                                                                        string
	skills                                                                             Skills
	project, loadedProject, targets                                                    string
	skillSelection                                                                     map[string]bool
	skillDirty                                                                         bool
	memories                                                                           []Memory
	memoryFilter                                                                       string
	memoryScope, memoryKey, memoryText                                                 string
	prompt                                                                             Prompt
	promptDraft                                                                        string
	configs                                                                            map[string]ClientConfig
	configSelection                                                                    map[string]map[string]bool
	feedback                                                                           []Feedback
	chatInput, chatReply, chatUser, chatStatus, feedbackNote                           string
	chatModels                                                                         []string
	chatModel, chatDefault                                                             string
	chatList                                                                           ui.ScrollState
	editorOpen                                                                         bool
	editorKind, editorTitle, editorID, editorName, editorURL, editorToken, editorRoute string
	editorCaps                                                                         map[string]bool
	editorModalities                                                                   string
	deleteOpen                                                                         bool
	deleteAction                                                                       func()
	browserOpen                                                                        bool
	browserPath, browserParent                                                         string
	browserDirs                                                                        []string
}

func New(store *connection.Store) *App {
	a := &App{store: store, profiles: store.State(), page: "connections", logLimit: "50", logSelected: -1,
		memoryScope: "global", memoryFilter: "all", skillSelection: map[string]bool{}, configs: map[string]ClientConfig{}, configSelection: map[string]map[string]bool{}}
	if a.profiles.ActiveID != "" {
		a.page = "overview"
	}
	return a
}
func (a *App) SetDispatcher(fn func(func())) { a.dispatch = fn }
func (a *App) Close()                        { a.stop(); a.dispatch = nil }
func (a *App) Start() {
	if a.page != "connections" {
		a.reload()
	}
}
func (a *App) ShowConnections() { a.navigate("connections") }
func (a *App) stop() {
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
	a.work("正在读取", 35*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		switch page {
		case "overview":
			var d Dashboard
			err := c.JSON(ctx, "GET", "/dashboard", nil, &d)
			return func() { a.dashboard = d; a.loaded = true }, err
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
				Default string   `json:"default"`
				Models  []string `json:"models"`
			}
			err := c.JSON(ctx, "GET", "/assistant/models", nil, &m)
			return func() {
				a.chatModels = m.Models
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
	a.reload()
}

const chatDefaultOption = "网关默认"

// pickNativeFolder 可在测试中替换；生产环境打开 macOS 原生目录面板。
var pickNativeFolder = nativefolder.Pick

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

// pickLocalFolder 用原生面板选择本机目录；取消则保持原值不变。
func (a *App) pickLocalFolder() {
	path, ok := pickNativeFolder()
	if !ok {
		return
	}
	if trimmed := strings.TrimSpace(path); trimmed != "" {
		a.project = trimmed
	}
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
	a.logs = nil
	a.memories = nil
	a.skills = Skills{}
	a.skillDirty = false
	a.loadedProject = ""
	a.project = ""
	a.targets = ""
	a.skillSelection = map[string]bool{}
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
	a.browserOpen = true
	a.browseHost(strings.TrimSpace(a.project))
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
	a.project = a.browserPath
	a.browserOpen = false
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
