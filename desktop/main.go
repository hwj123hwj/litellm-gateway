package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/connection"
)

// Gateway exposes connection operations, never stored management tokens.
type Gateway struct{ store *connection.Store }

func (g *Gateway) Config() connection.State { return g.store.State() }
func (g *Gateway) Save(ctx context.Context, in connection.Input) (connection.State, error) {
	return g.store.Save(ctx, in)
}
func (g *Gateway) Test(ctx context.Context, in connection.Input) error { return g.store.Test(ctx, in) }
func (g *Gateway) Select(id string) (connection.State, error)          { return g.store.Select(id) }
func (g *Gateway) Delete(id string) (connection.State, error)          { return g.store.Delete(id) }

func main() {
	app := mygo.App
	app.SetName("Gateway")
	dir, err := app.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	// Isolated config is useful for smoke tests without touching user profiles.
	if override := os.Getenv("GATEWAY_DESKTOP_CONFIG_DIR"); override != "" {
		dir = override
	}
	store, err := connection.New(filepath.Join(dir, "connections.json"))
	if err != nil {
		log.Fatal(err)
	}
	mygo.Bind(&Gateway{store})
	if err = mygo.Protocol.Handle("gateway", store); err != nil {
		log.Fatal(err)
	}
	if !app.RequestSingleInstanceLock() {
		return
	}
	var win *mygo.Window
	show := func() {
		if win == nil || win.IsDestroyed() {
			win = mygo.NewWindow(mygo.WindowOptions{Title: "Gateway", URL: "/", Width: 1280, Height: 850, MinWidth: 800, MinHeight: 600,
				TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 48, TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
				BackgroundColor: "light-dark(#f2f5f3, #0d1112)", StateKey: "main"})
			win.Page().OnPageTitleUpdated(func(e *mygo.TitleEvent) { e.PreventDefault() })
			win.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
				if strings.HasPrefix(e.URL, "http://") || strings.HasPrefix(e.URL, "https://") {
					e.PreventDefault()
					_ = mygo.Shell.OpenExternal(e.URL)
				}
			})
		}
		win.Show()
		win.Focus()
	}
	connections := func(*mygo.MenuItem, *mygo.Window) {
		show()
		page := win.Page()
		// WKWebView's native fragment navigation does not reliably dispatch
		// hashchange. Navigate inside the page so React observes the change.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := page.EvalContext(ctx, "location.hash = '/connections'"); err != nil {
				log.Printf("打开连接设置: %v", err)
				_ = page.LoadURL("/#/connections")
			}
		}()
	}
	app.OnActivate(func(bool) { show() })
	app.OnSecondInstance(func([]string, string) { show() })
	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu},
			{Label: "网关", Submenu: []*mygo.MenuItem{
				{Label: "显示控制台", Accelerator: "CmdOrCtrl+1", Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
				{Label: "管理连接…", Accelerator: "CmdOrCtrl+,", Click: connections},
			}}, {Role: mygo.RoleViewMenu}, {Role: mygo.RoleWindowMenu},
		}))
		tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Gateway · 网关管理", Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开 Gateway", Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
			{Label: "管理连接…", Click: connections}, mygo.Separator(), {Role: mygo.RoleQuit},
		})})
		if err != nil {
			log.Printf("托盘不可用: %v", err)
		} else {
			_ = tray
			app.OnWindowAllClosed(func() {})
		}
		show()
	})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
}

func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 6; y < 26; y++ {
		for x := 6; x < 26; x++ {
			if (y < 10 || y >= 22 || x < 10 || (x >= 18 && y >= 15)) && !(x >= 20 && y >= 10 && y < 15) {
				img.Set(x, y, color.Black)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
