package console

import (
	"context"
	"encoding/json"
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
	a.editorRoute = "beta, alpha"
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
	tt := ui.NewTester(a.View, 1100, 800)
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

func TestLocalNativePickFillsProjectPath(t *testing.T) {
	a, queue, _ := fixture(t)
	a.page = "skills"
	a.loaded = true
	if !a.localConnection() {
		t.Fatal("fixture connection should count as local")
	}
	previous := a.PickFolder
	a.PickFolder = func(done func(path string, ok bool)) { done("/Users/demo/project", true) }
	defer func() { a.PickFolder = previous }()
	tt := ui.NewTester(a.View, 1100, 800)
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
