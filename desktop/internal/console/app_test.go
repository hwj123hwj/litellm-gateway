package console

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/connection"
)

func fixture(t *testing.T) (*App, chan func(), chan string) {
	t.Helper()
	requests := make(chan string, 30)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer native-test" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" {
			var body any
			json.NewDecoder(r.Body).Decode(&body)
			data, _ := json.Marshal(body)
			requests <- r.Method + " " + r.URL.Path + " " + string(data)
		}
		switch r.URL.Path {
		case "/admin/fs/dirs":
			if r.URL.Query().Get("path") == "/tmp/root/sub" {
				w.Write([]byte(`{"path":"/tmp/root/sub","parent":"/tmp/root","dirs":[]}`))
			} else {
				w.Write([]byte(`{"path":"/tmp/root","parent":"/tmp","dirs":["sub"]}`))
			}
		case "/admin/models":
			w.Write([]byte(`{"models":[{"model":"coding","provider":"alpha","providers":["alpha","beta"],"status":"online","capabilities":["text","audio_transcription"],"input_modalities":["text"]}]}`))
		case "/admin/routes":
			w.Write([]byte(`{"routes":[{"model":"coding","providers":[{"name":"alpha"},{"name":"beta"}]}]}`))
		case "/admin/skills/market/browse":
			if r.URL.Query().Get("q") != "" {
				w.Write([]byte(`{"enabled":true,"skills":[],"total":0,"page":1}`))
				return
			}
			w.Write([]byte(`{"enabled":true,"skills":[` +
				`{"id":5,"name":"market-demo","display_name":"市场示例","description":"来自市场的演示技能","version":"1.0.0","install_count":9,"tarball_size":2048},` +
				`{"id":6,"name":"installed-demo","display_name":"已安装示例","description":"已经装好的技能","version":"0.9.0","install_count":4,"tarball_size":1024}` +
				`],"total":2,"page":1}`))
		case "/admin/skills/market/installed":
			w.Write([]byte(`{"enabled":true,"skills_dir":"/tmp/skills","installed":[{"name":"installed-demo","display_name":"已安装示例","version":"0.9.0"}]}`))
		default:
			w.Write([]byte(`{"status":"ok","summary":{"today_requests":24,"success_rate":98.5},"providers":[],"models":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	store, err := connection.New(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Save(context.Background(), connection.Input{Name: "测试网关", URL: srv.URL, Token: "native-test"}); err != nil {
		t.Fatal(err)
	}
	a := New(store)
	queue := make(chan func(), 100)
	a.SetDispatcher(func(fn func()) { queue <- fn })
	t.Cleanup(a.Close)
	return a, queue, requests
}

// drainDispatcher 执行 dispatcher 队列中待处理的 UI 回调（如 sheet 完成
// 回调经 dispatch 回填的字段赋值）。
func drainDispatcher(a *App, queue chan func()) {
	for {
		select {
		case fn := <-queue:
			fn()
		default:
			return
		}
	}
}

func await(t *testing.T, a *App, queue chan func()) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for a.busy != "" {
		select {
		case fn := <-queue:
			fn()
		case <-deadline:
			t.Fatal("UI operation timed out")
		}
	}
	if a.problem != "" {
		t.Fatal(a.problem)
	}
}
func click(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	if err := tt.Click(text); err != nil {
		t.Fatal(err)
	}
}
func TestNativeModelEditorSendsCapabilitiesAndRoute(t *testing.T) {
	a, queue, requests := fixture(t)
	a.page = "models"
	a.reload()
	await(t, a, queue)
	tt := ui.NewTester(a.View, 1100, 800)
	click(t, tt, "coding 能力设置")
	click(t, tt, "vision")
	click(t, tt, "保存")
	await(t, a, queue)
	tt.Frame()
	select {
	case got := <-requests:
		if !strings.Contains(got, `PUT /admin/models/coding {"capabilities":["audio_transcription","text","vision"],"input_modalities":["text"]}`) {
			t.Fatal(got)
		}
	default:
		t.Fatal("model save not sent")
	}
	click(t, tt, "coding 路由顺序")
	click(t, tt, "下移 alpha") // [alpha, beta] → [beta, alpha]
	tt.Frame()
	click(t, tt, "保存")
	await(t, a, queue)
	select {
	case got := <-requests:
		if !strings.Contains(got, `PUT /admin/routes/coding {"providers":["beta","alpha"]}`) {
			t.Fatal(got)
		}
	default:
		t.Fatal("route save not sent")
	}
}
func TestLateCompletionCannotRestoreOldHost(t *testing.T) {
	a, queue, _ := fixture(t)
	released := make(chan struct{})
	started := make(chan struct{})
	a.launch("slow", time.Second, func(context.Context) (func(), error) {
		close(started)
		<-released
		return func() { a.chatReply = "old host" }, nil
	})
	<-started
	a.stop()
	a.resetHost()
	close(released)
	select {
	case fn := <-queue:
		fn()
	case <-time.After(time.Second):
		t.Fatal("no completion")
	}
	if a.chatReply != "" || a.busy != "" {
		t.Fatal("stale host response applied")
	}
}
func TestNativeViewsAndVirtualLists(t *testing.T) {
	a, _, _ := fixture(t)
	a.loaded = true
	a.dashboard = Dashboard{Summary: Summary{TodayRequests: 1250, SuccessRate: 99.5, Uptime: "2h", ActiveModels: 8}, Providers: []Provider{{Name: "alpha", Status: "online", Requests: 1250}}}
	a.providers = []Provider{{Name: "alpha", State: "closed", Status: "online", HasProbe: true, ProbeDetail: "上游可用"}}
	for i := 0; i < 1000; i++ {
		a.models = append(a.models, Model{Name: strings.Repeat("m", 12) + shortNumber(int64(i)), Provider: "alpha", Status: "online", Capabilities: []string{"text"}})
	}
	a.skills = Skills{Configured: true, Targets: []string{"/tmp/skills"}, Skills: []Skill{{ID: "test-skill", Name: "代码评审", Description: "检查实现与边界"}}, Manifest: "/tmp/manifest.json"}
	a.acceptSkills(a.skills)
	a.memories = []Memory{{ID: 1, Scope: "global", Status: "candidate", Statement: "偏好原生 Go 界面"}}
	a.logs = []Log{{Timestamp: "2026-10-10 12:00", Model: "coding", Provider: "alpha", RequestID: "test", Code: 200}}
	for _, target := range []string{"pi", "zcode", "harness"} {
		a.configs[target] = ClientConfig{Path: "/tmp/" + target + ".json", Desired: []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{{ID: "coding"}}}
	}
	a.initConfigSelection()
	for _, page := range pages {
		a.page = page.ID
		for _, size := range []struct{ w, h int }{{1280, 850}, {900, 680}} {
			tt := ui.NewTester(a.View, size.w, size.h)
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				if !tt.HasText(page.Title) {
					t.Fatal("missing page", page.ID)
				}
				if page.ID == "models" && len(tt.Texts()) > 200 {
					t.Fatal("list not virtualized", len(tt.Texts()))
				}
				if dir := os.Getenv("GATEWAY_QA_SCREENSHOTS"); dir != "" && size.w == 1280 {
					os.MkdirAll(dir, 0700)
					name := page.ID + "-light.png"
					if dark {
						name = page.ID + "-dark.png"
					}
					f, err := os.Create(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if err = png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
					f.Close()
				}
			}
		}
	}
}

func TestSkillsProjectBrowserPicksHostDirectory(t *testing.T) {
	a, queue, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	seedSkills(a)
	tt := ui.NewTester(a.View, 1100, 800)
	click(t, tt, "展开安装范围设置")
	click(t, tt, "浏览项目路径")
	await(t, a, queue)
	tt.Frame()
	if a.browserPath != "/tmp/root" || !a.browserOpen {
		t.Fatal("browser did not open at gateway home:", a.browserPath)
	}
	click(t, tt, "打开 sub")
	await(t, a, queue)
	tt.Frame()
	click(t, tt, "选择此目录")
	if a.browserOpen || a.project != "/tmp/root/sub" {
		t.Fatal("directory not picked:", a.project, a.browserOpen)
	}
}

// seedSkills 配置一个可用的技能库，供技能页交互测试使用。
func seedSkills(a *App) {
	a.skills = Skills{Configured: true, Targets: []string{"/tmp/skills"}, Skills: []Skill{
		{ID: "review", Name: "代码评审", Description: "检查实现与边界", Author: "hwj", Tags: []string{"开发工具"}},
		{ID: "slides", Name: "演示制作", Description: "生成演示幻灯片", Tags: []string{"演示与设计"}},
	}, Manifest: "/tmp/manifest.json"}
	a.acceptSkills(a.skills)
}

func TestLocalNativePickFillsProjectPath(t *testing.T) {
	a, queue, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	seedSkills(a)
	if !a.localConnection() {
		t.Fatal("fixture connection should count as local")
	}
	previous := a.PickFolder
	a.PickFolder = func(done func(path string, ok bool)) { done("/Users/demo/project", true) }
	defer func() { a.PickFolder = previous }()
	tt := ui.NewTester(a.View, 1100, 800)
	click(t, tt, "展开安装范围设置")
	click(t, tt, "本机选择目录")
	drainDispatcher(a, queue)
	if a.project != "/Users/demo/project" {
		t.Fatal("native pick not applied:", a.project)
	}
	a.PickFolder = func(done func(path string, ok bool)) { done("", false) }
	click(t, tt, "本机选择目录")
	drainDispatcher(a, queue)
	if a.project != "/Users/demo/project" {
		t.Fatal("cancel must keep the old value:", a.project)
	}
}

func TestSkillsSearchAndTagFilter(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	seedSkills(a)
	tt := ui.NewTester(a.View, 1100, 800)
	a.skillFilter = "评审"
	tt.Frame()
	if !tt.HasText("代码评审") {
		t.Fatal("search filter hid the matching skill")
	}
	if tt.HasText("演示制作") {
		t.Fatal("search filter did not hide unmatched skill")
	}
	a.skillFilter = ""
	a.skillTag = "演示与设计"
	tt.Frame()
	if !tt.HasText("演示制作") {
		t.Fatal("tag filter hid the matching skill")
	}
	if tt.HasText("代码评审") {
		t.Fatal("tag filter did not hide untagged skill")
	}
}

func TestSkillsRowDetailExpansion(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	seedSkills(a)
	tt := ui.NewTester(a.View, 1100, 800)
	if tt.HasText("作者 hwj") {
		t.Fatal("detail metadata should stay hidden before expansion")
	}
	click(t, tt, "技能详情 review")
	tt.Frame()
	if !tt.HasText("作者 hwj") {
		t.Fatal("expanded row should show skill metadata")
	}
	click(t, tt, "技能详情 review")
	tt.Frame()
	if tt.HasText("作者 hwj") {
		t.Fatal("collapsed row should hide skill metadata")
	}
}

func TestSkillsSaveSendsConfigThenSync(t *testing.T) {
	a, queue, requests := fixture(t)
	a.page = "skills"
	a.loaded = true
	seedSkills(a)
	tt := ui.NewTester(a.View, 1100, 800)
	click(t, tt, "代码评审") // 勾选启用
	if !a.skillDirty {
		t.Fatal("selection change should mark the manifest dirty")
	}
	click(t, tt, "保存并同步")
	await(t, a, queue)
	config, sync := <-requests, <-requests
	if !strings.Contains(config, "PUT /admin/skills/config") || !strings.Contains(config, `"enabled":["review"]`) {
		t.Fatal("config PUT missing or wrong:", config)
	}
	if !strings.Contains(sync, "POST /admin/skills/sync") {
		t.Fatal("sync POST missing:", sync)
	}
	if a.skillDirty {
		t.Fatal("successful save should clear the dirty flag")
	}
}

func TestSkillsMarketBrowseAndInstall(t *testing.T) {
	a, queue, requests := fixture(t)
	a.page = "skills"
	a.loaded = true
	a.skillSource = skillSourceMarket
	a.reload()
	await(t, a, queue)
	tt := ui.NewTester(a.View, 1100, 800)
	if !tt.HasText("市场示例") || !tt.HasText("已安装示例") {
		t.Fatal("market browse rows missing")
	}
	if !tt.HasText("已安装") {
		t.Fatal("installed chip missing for installed skill")
	}
	click(t, tt, "安装技能 市场示例")
	await(t, a, queue)
	select {
	case got := <-requests:
		if !strings.Contains(got, "POST /admin/skills/market/install") || !strings.Contains(got, `"id":5`) {
			t.Fatal("install request wrong:", got)
		}
	default:
		t.Fatal("install request not sent")
	}
}

func TestSkillsMarketUninstallRequiresConfirm(t *testing.T) {
	a, queue, requests := fixture(t)
	a.page = "skills"
	a.loaded = true
	a.skillSource = skillSourceMarket
	a.reload()
	await(t, a, queue)
	tt := ui.NewTester(a.View, 1100, 800)
	click(t, tt, "卸载技能 已安装示例")
	tt.Frame()
	if !tt.HasText("从本机卸载") {
		t.Fatal("uninstall confirmation dialog missing")
	}
	click(t, tt, "移除")
	await(t, a, queue)
	select {
	case got := <-requests:
		if !strings.Contains(got, "POST /admin/skills/market/uninstall") || !strings.Contains(got, `"name":"installed-demo"`) {
			t.Fatal("uninstall request wrong:", got)
		}
	default:
		t.Fatal("uninstall request not sent")
	}
}

func TestSkillsMarketDisabledShowsHint(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	a.skillSource = skillSourceMarket
	a.market = MarketPage{Hint: "技能市场未启用：设置 SKILLS_MARKET_ENABLED 后重启网关"}
	tt := ui.NewTester(a.View, 1100, 800)
	if !tt.HasText("技能市场未启用") {
		t.Fatal("disabled market hint missing")
	}
	if tt.HasText("搜索市场") {
		t.Fatal("market controls should stay hidden when disabled")
	}
}

func TestModelPickerSearchFilterAndChoose(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "assistant"
	a.loaded = true
	for i := 0; i < 20; i++ {
		a.chatModels = append(a.chatModels, fmt.Sprintf("cloudcode/gemini-3.8-flash-%03d", i))
		a.chatModels = append(a.chatModels, fmt.Sprintf("mini-max/m%03d-model", i))
	}
	a.chatModel = chatDefaultOption
	tt := ui.NewTester(a.View, 1280, 850)
	click(t, tt, "选择模型，当前 "+chatDefaultOption)
	tt.Frame()
	if !a.modelPickerOpen {
		t.Fatal("picker should open")
	}
	if !tt.HasText("共 41 个模型，显示 41 个") {
		t.Fatal("picker count missing")
	}
	if !tt.HasText("当前") {
		t.Fatal("current selection chip missing")
	}
	a.modelPickerFilter = "gem"
	tt.Frame()
	if !tt.HasText("cloudcode/gemini-3.8-flash-000") {
		t.Fatal("filter hid matching models")
	}
	if tt.HasText("mini-max/m000-model") {
		t.Fatal("filter kept non-matching models")
	}
	click(t, tt, "选择模型 cloudcode/gemini-3.8-flash-003")
	tt.Frame()
	if a.chatModel != "cloudcode/gemini-3.8-flash-003" || a.modelPickerOpen {
		t.Fatal("choosing did not apply and close:", a.chatModel, a.modelPickerOpen)
	}
	if a.modelPickerFilter != "" {
		t.Fatal("filter should reset after closing")
	}
}

func TestModelPickerEmptyState(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "assistant"
	a.loaded = true
	a.chatModels = []string{"glm-5.3-flash"}
	a.modelPickerOpen = true
	a.modelPickerFilter = "不存在的模型"
	tt := ui.NewTester(a.View, 1280, 850)
	if !tt.HasText("没有匹配的模型") {
		t.Fatal("empty state missing")
	}
}

func TestModelsCategoryFilter(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "models"
	a.loaded = true
	a.models = []Model{
		{Name: "chat", Provider: "alpha", Status: "online", Capabilities: []string{"text", "tool_calling"}},
		{Name: "BAAI/bge-m3", Provider: "alpha", Status: "online", Capabilities: []string{"embedding", "text"}},
		{Name: "asr-v3", Provider: "alpha", Status: "online", Capabilities: []string{"audio_transcription", "text"}},
		{Name: "vision-chat", Provider: "beta", Status: "online", Capabilities: []string{"text", "vision", "reasoning"}},
	}
	tt := ui.NewTester(a.View, 1280, 850)
	if !tt.HasText("BAAI/bge-m3") || !tt.HasText("asr-v3") {
		t.Fatal("all models should show without a category")
	}
	a.modelCategory = "向量"
	tt.Frame()
	if !tt.HasText("BAAI/bge-m3") {
		t.Fatal("embedding category hid the embedding model")
	}
	if tt.HasText("asr-v3") || tt.HasText("vision-chat") {
		t.Fatal("embedding category kept non-embedding models")
	}
	a.modelCategory = "语音转写"
	tt.Frame()
	if !tt.HasText("asr-v3") || tt.HasText("BAAI/bge-m3") {
		t.Fatal("transcription category filter wrong")
	}
	a.modelCategory = modelCategoryAll
	tt.Frame()
	if !tt.HasText("chat") || !tt.HasText("vision-chat") {
		t.Fatal("resetting the category lost models")
	}
}

func TestMemoryScopeKeyControls(t *testing.T) {
	a, queue, _ := fixture(t)
	a.page = "memory"
	a.loaded = true
	a.memories = []Memory{{ID: 7, Scope: "client", ScopeKey: "claude-code", Status: "active", Statement: "x"}}
	a.memoryScope = "client"
	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()
	click(t, tt, "客户端")
	if !tt.HasText("claude-code") || !tt.HasText("pi") {
		t.Fatal("client suggestions missing")
	}
	a.memoryScope = "project"
	saved := a.PickFolder
	a.PickFolder = func(done func(path string, ok bool)) { done("/tmp/picked-native", true) }
	tt.Frame()
	if !tt.HasText("浏览记忆项目") || !tt.HasText("本机选择记忆项目") {
		t.Fatal("project scope controls missing")
	}
	click(t, tt, "本机选择记忆项目")
	drainDispatcher(a, queue)
	a.PickFolder = saved
	tt.Frame()
	if a.memoryKey != "/tmp/picked-native" {
		t.Fatal("native pick did not fill memory key:", a.memoryKey)
	}
	a.profiles.Profiles[0].URL = "https://gateway.example.com"
	tt.Frame()
	click(t, tt, "浏览记忆项目")
	await(t, a, queue)
	tt.Frame()
	click(t, tt, "选择此目录")
	if a.browserOpen || a.memoryKey != "/tmp/root" {
		t.Fatal("remote browser did not fill memory key:", a.memoryKey, a.browserOpen)
	}
}

func TestDirtySkillsPreventScopeAndHostNavigation(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "skills"
	a.skillDirty = true
	a.loadedProject = "/old/project"
	tt := ui.NewTester(a.View, 1000, 800)
	a.navigate("connections")
	if a.page != "skills" || a.loadedProject != "/old/project" || a.problem == "" {
		t.Fatal("unsaved skill selection lost")
	}
	tt.Frame()
	if !tt.HasText("技能选择尚未保存") {
		t.Fatal("missing unsaved selection guidance")
	}
}

func TestClientSyncBadgeTracksEditedSelection(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "settings"
	a.loaded = true
	for _, target := range []string{"pi", "zcode", "harness"} {
		cfg := ClientConfig{Exists: true, InSync: true, Current: []string{"coding"}}
		cfg.Desired = append(cfg.Desired, struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{ID: "coding"})
		a.configs[target] = cfg
	}
	a.initConfigSelection()
	tt := ui.NewTester(a.View, 1280, 1200)
	if tt.HasText("待同步") {
		t.Fatal("matching selections should be synced")
	}
	click(t, tt, "清空 pi")
	tt.Frame()
	if !tt.HasText("待同步") {
		t.Fatal("changed selection still claims to be synced")
	}
	click(t, tt, "全选 pi")
	tt.Frame()
	if tt.HasText("待同步") {
		t.Fatal("restored selection should match the saved configuration")
	}
}

func TestAssistantUsesAuthoritativeFinalReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"text_delta\",\"content\":\"partial\"}\n\ndata: {\"type\":\"done\",\"content\":\"complete reply\"}\n\n"))
	}))
	defer srv.Close()
	s, _ := connection.New(filepath.Join(t.TempDir(), "connections.json"))
	if _, err := s.Save(context.Background(), connection.Input{Name: "Stream", URL: srv.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	a := New(s)
	queue := make(chan func(), 20)
	a.SetDispatcher(func(fn func()) { queue <- fn })
	defer a.Close()
	a.page = "assistant"
	a.chatInput = "hello"
	a.sendChat()
	await(t, a, queue)
	if a.chatReply != "complete reply" || a.chatStatus != "回复完成" {
		t.Fatal(a.chatReply, a.chatStatus)
	}
}
