package console

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/connection"
)

var pages = []struct{ ID, Title, Subtitle string }{
	{"overview", "运行概览", "网关流量、模型与上游健康状态"},
	{"models", "模型与路由", "配置模型能力和 Provider 回退顺序"},
	{"providers", "Provider", "管理上游连接、探测与熔断器"},
	{"logs", "请求日志", "查看请求结果和每次上游尝试"},
	{"skills", "技能", "管理全局与项目技能的安装清单"},
	{"memory", "记忆", "确认、停用和管理助理记忆"},
	{"assistant", "助理", "与网关助理对话，实时查看回复和工具状态"},
	{"settings", "设置", "客户端模型同步、助理人设与反馈"},
	{"connections", "网关连接", "管理本机与远程网关的连接"},
}

func (a *App) View(c *ui.Context) {
	theme := *c.Theme()
	theme.Accent = ui.Hex("#a75720")
	theme.AccentText = ui.Hex("#ffffff")
	theme.AccentHover = ui.Hex("#8a441a")
	theme.AccentPressed = ui.Hex("#703716")
	theme.Focus = theme.Accent.Alpha(.4)
	theme.Selection = theme.Accent.Alpha(.25)
	if !theme.Dark {
		theme.Background = ui.Hex("#f7f8fa")
		theme.Surface = ui.Hex("#eef0f3")
		theme.Border = ui.Hex("#dce0e5")
		theme.TextMuted = ui.Hex("#5b6470")
	}
	theme.Radius = 8
	c.SetTheme(&theme)
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Height(max(bar.Height, 48)).Padding(0, 16, 0, bar.Left+16).Gap(12).DragWindow().BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			ui.Box(c).Size(28, 28).Center().Radius(8).Background(theme.Accent).AlignSelf(ui.Center).Children(func() {
				ui.Text(c, "E").Bold().FontSize(15).TextColor(theme.AccentText)
			})
			ui.Text(c, "EasyGateway").Bold().FontSize(16)
			ui.Spacer(c)
			profile := a.active()
			if profile.ID != "" {
				dot := theme.TextMuted
				switch {
				case a.problem != "":
					dot = theme.Danger
				case a.busy != "":
					dot = theme.Accent
				case a.loaded:
					dot = okGreen
				}
				ui.Box(c).Size(9, 9).Radius(999).Background(dot).AlignSelf(ui.Center)
				ui.Text(c, profile.Name+" · "+profile.URL).SingleLine().TextColor(theme.TextMuted)
			}
		})
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(208).Background(theme.Surface).BorderWidth(0, 1, 0, 0).BorderColor(theme.Border).Children(func() {
				current := a.page
				sidebar := ui.Sidebar(c, &current, func() {
					ui.SidebarSection(c, "控制台", nil, func() {
						for _, p := range pages[:4] {
							ui.SidebarItem(c, p.ID, nil, p.Title)
						}
					})
					ui.SidebarSection(c, "个人工作区", nil, func() {
						for _, p := range pages[4:8] {
							ui.SidebarItem(c, p.ID, nil, p.Title)
						}
					})
					ui.SidebarSection(c, "连接", nil, func() { ui.SidebarItem(c, "connections", nil, "网关连接") })
				}).Grow(1).Padding(12, 8)
				if sidebar.Changed() {
					if a.active().ID == "" && current != "connections" {
						a.problem = "请先添加一个网关连接"
					} else {
						a.navigate(current)
					}
				}
				ui.Text(c, "原生桌面端 · "+a.Version).FontSize(12).TextColor(theme.TextMuted).Padding(16)
			})
			ui.Column(c).Grow(1).MinWidth(0).Padding(24, 28).Gap(16).Children(func() {
				ui.Row(c).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						for _, p := range pages {
							if p.ID == a.page {
								ui.Text(c, p.Title).FontSize(26).Bold()
								muted(c, p.Subtitle)
							}
						}
					})
					if a.page != "connections" {
						ui.Button(c, "刷新").Disabled(a.busy != "" || a.skillDirty).OnClick(a.reload)
					}
					if a.busy != "" {
						ui.Button(c, "取消").OnClick(a.stop)
					}
				})
				ui.Box(c).Height(3).Children(func() {
					if a.busy != "" {
						ui.Progress(c, -1).Label(a.busy)
					}
				})
				if a.problem != "" {
					ui.Row(c).Gap(12).Padding(12).Radius(8).Background(theme.Danger.Alpha(.1)).Children(func() {
						ui.Text(c, a.problem).TextColor(theme.Danger).Grow(1).Selectable()
						ui.Button(c, "关闭提示").OnClick(func() { a.problem = "" })
					})
				}
				if a.notice != "" {
					ui.Row(c).Gap(12).Padding(12).Radius(8).Background(theme.Accent.Alpha(.08)).Children(func() {
						ui.Text(c, a.notice).TextColor(theme.Accent).Grow(1).Selectable()
						ui.Button(c, "收起").OnClick(func() { a.notice = "" })
					})
				}
				if a.page != "connections" && !a.loaded {
					ui.Column(c).Grow(1).Center().Children(func() {
						text := "暂未加载数据，点击刷新重试"
						if a.busy != "" {
							text = "正在载入网关数据"
						}
						muted(c, text)
					})
					return
				}
				switch a.page {
				case "connections":
					a.connectionsView(c)
				case "overview":
					a.overviewView(c)
				case "models":
					a.modelsView(c)
				case "providers":
					a.providersView(c)
				case "logs":
					a.logsView(c)
				case "skills":
					a.skillsView(c)
				case "memory":
					a.memoryView(c)
				case "assistant":
					a.assistantView(c)
				case "settings":
					a.settingsView(c)
				}
			})
		})
	})
	a.editor(c)
	a.browserModal(c)
	if ui.AlertDialog(c, &a.deleteOpen, "确认移除？", a.editorTitle, "取消", "移除") == 1 && a.deleteAction != nil {
		action := a.deleteAction
		a.deleteAction = nil
		action()
	}
}
func muted(c *ui.Context, text string) { ui.Text(c, text).TextColor(c.Theme().TextMuted) }
func chip(c *ui.Context, text string, color ui.Color) {
	ui.Box(c).Padding(2, 10).Radius(999).Background(color.Alpha(.13)).Shrink(0).Children(func() {
		ui.Text(c, text).TextColor(color).FontSize(12).SingleLine()
	})
}
func card(c *ui.Context, body func()) {
	ui.Column(c).Padding(18).Gap(12).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(12).Children(body)
}
func row(c *ui.Context, body func()) {
	ui.Column(c).Padding(14, 4, 14, 0).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border.Alpha(.7)).Children(body)
}
func meta(c *ui.Context, parts ...string) {
	ui.Row(c).Gap(16).Children(func() {
		for _, part := range parts {
			ui.Text(c, part).TextColor(c.Theme().TextMuted).SingleLine()
		}
	})
}
func empty(c *ui.Context, text string) {
	ui.Column(c).Grow(1).Center().Children(func() { ui.Text(c, text).FontSize(14).TextColor(c.Theme().TextMuted) })
}
func (a *App) connectionsView(c *ui.Context) {
	ui.Row(c).Gap(12).Children(func() {
		ui.PrimaryButton(c, "添加连接").Disabled(a.busy != "").OnClick(func() { a.editConnection(connection.Profile{}) })
		muted(c, "Token 保存在本机受保护配置中")
	})
	if len(a.profiles.Profiles) == 0 {
		card(c, func() {
			ui.Text(c, "连接你的网关").FontSize(22).Bold()
			muted(c, "填写网关地址和管理 Token。验证通过后，即可管理模型、路由和工作区。")
			muted(c, "支持本机、远程主机与带路径前缀的反向代理。")
		})
		return
	}
	ui.List(c, nil, len(a.profiles.Profiles), func(i int) {
		p := a.profiles.Profiles[i]
		ui.Column(c.Key(p.ID)).Children(func() {
			row(c, func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, p.Name).FontSize(18).Bold()
						ui.Text(c, p.URL).Selectable().TextColor(c.Theme().TextMuted)
					})
					if p.ID == a.profiles.ActiveID {
						chip(c, "当前连接", c.Theme().Accent)
					} else {
						ui.PrimaryButton(c, "使用").Label("使用 " + p.Name).Disabled(a.busy != "").OnClick(func() { a.selectConnection(p.ID) })
					}
					ui.Button(c, "编辑").Label("编辑 " + p.Name).Disabled(a.busy != "").OnClick(func() { a.editConnection(p) })
					ui.Button(c, "移除").Label("移除 " + p.Name).Disabled(a.busy != "").OnClick(func() {
						a.editorTitle = "移除连接「" + p.Name + "」及其本机 Token，不会删除网关数据。"
						a.deleteAction = func() {
							st, err := a.store.Delete(p.ID)
							if err != nil {
								a.problem = err.Error()
								return
							}
							a.stop()
							a.profiles = st
							a.resetHost()
						}
						a.deleteOpen = true
					})
				})
			})
		})
	}).Grow(1)
}
func (a *App) editConnection(p connection.Profile) {
	a.editorKind = "connection"
	a.editorID = p.ID
	a.editorName = p.Name
	a.editorURL = p.URL
	a.editorToken = ""
	a.editorOpen = true
	a.problem = ""
}
func input(c *ui.Context, label string, value *string, hint string) {
	ui.Field(c, label, func() { ui.TextInput(c, value).Placeholder(hint).FillWidth() })
}
func (a *App) editor(c *ui.Context) {
	wasOpen := a.editorOpen
	ui.Modal(c, &a.editorOpen, func() {
		ui.Column(c).Width(560).Gap(16).Children(func() {
			title := "模型能力"
			if a.editorKind == "connection" {
				title = "网关连接"
			} else if a.editorKind == "route" {
				title = "回退路由"
			}
			ui.Text(c, title).FontSize(22).Bold()
			ui.Fieldset(c, "", func() {
				switch a.editorKind {
				case "connection":
					input(c, "连接名称", &a.editorName, "例如：本机网关")
					input(c, "网关地址", &a.editorURL, "http://127.0.0.1:4001")
					ui.Field(c, "管理 Token", func() {
						ui.TextInput(c, &a.editorToken).Password().FillWidth().Placeholder("ADMIN_TOKEN / LITELLM_MASTER_KEY")
					})
					if a.editorID != "" {
						muted(c, "Token 留空保留已有值。填写新值即可替换。")
					}
				case "model":
					ui.Text(c, a.editorID).Bold().Selectable()
					ui.Row(c).Wrap().Gap(12).Children(func() {
						for _, cap := range capabilityChoices(a.editorCaps) {
							value := a.editorCaps[cap]
							if ui.Checkbox(c.Key(cap), &value, cap).Changed() {
								a.editorCaps[cap] = value
							}
						}
					})
					input(c, "输入模态", &a.editorModalities, "text, image, audio, video, file")
				case "route":
					ui.Text(c, a.editorID).Bold().Selectable()
					input(c, "Provider 顺序", &a.editorRoute, "多个名称用逗号分隔")
					muted(c, "从左到右依次尝试，只填写该模型已配置的 Provider。")
					for _, r := range a.routes {
						if r.Model == a.editorID {
							names := []string{}
							for _, p := range r.Providers {
								names = append(names, p.Name)
							}
							muted(c, "当前链："+strings.Join(names, " → "))
						}
					}
				}
			}).Disabled(a.busy != "")
			if a.problem != "" {
				ui.Text(c, a.problem).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消编辑").Disabled(a.busy != "").OnClick(func() { a.editorOpen = false; a.editorToken = "" })
				if a.editorKind == "connection" {
					ui.Button(c, "测试连接").Disabled(a.busy != "").OnClick(func() { a.saveConnection(false) })
				}
				ui.PrimaryButton(c, "保存").Disabled(a.busy != "").OnClick(a.saveEditor)
			})
		})
	})
	if wasOpen && !a.editorOpen && a.editorKind == "connection" {
		a.stop()
		a.editorToken = ""
	}
}

// browserModal 网关主机目录浏览器：点击目录名进入，"选择此目录"把当前
// 路径写回项目路径输入框。本机与远程连接走同一套 Admin API。
func (a *App) browserModal(c *ui.Context) {
	ui.Modal(c, &a.browserOpen, func() {
		theme := c.Theme()
		ui.Column(c).Width(620).Gap(14).Children(func() {
			ui.Text(c, "浏览网关主机目录").FontSize(20).Bold()
			muted(c, "浏览的是当前连接的网关主机上的目录；本机网关即本机目录。")
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.browserPath).FontSize(13).Grow(1).MinWidth(0).SingleLine().TextColor(theme.TextMuted)
				if a.browserParent != "" {
					ui.Button(c, "上级").Label("返回上级目录").Disabled(a.busy != "").OnClick(func() { a.browseHost(a.browserParent) })
				}
			})
			if len(a.browserDirs) == 0 {
				empty(c, "此目录没有子目录")
			}
			ui.List(c, nil, len(a.browserDirs), func(i int) {
				name := a.browserDirs[i]
				ui.Row(c.Key(name)).Padding(0, 0, 4).Children(func() {
					ui.Button(c, "打开 "+name).OnClick(func() { a.browseHost(a.browserPath + "/" + name) })
				})
			}).Grow(1).MinHeight(180).MaxHeight(380)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消浏览").OnClick(func() { a.browserOpen = false })
				ui.PrimaryButton(c, "选择此目录").Disabled(a.busy != "" || a.browserPath == "").OnClick(a.pickBrowserDir)
			})
		})
	})
}
func (a *App) saveConnection(save bool) {
	in := connection.Input{ID: a.editorID, Name: a.editorName, URL: a.editorURL, Token: a.editorToken}
	a.launch("正在验证连接", 20*time.Second, func(ctx context.Context) (func(), error) {
		if !save {
			err := a.store.Test(ctx, in)
			return func() { a.notice = "连接验证成功" }, err
		}
		commit, err := a.store.VerifySave(ctx, in)
		return func() {
			state, commitErr := commit()
			if commitErr != nil {
				a.problem = commitErr.Error()
				return
			}
			a.editorOpen = false
			a.editorToken = ""
			a.profiles = state
			a.resetHost()
			a.page = "overview"
			a.reload()
		}, err
	})
}

func capabilityChoices(chosen map[string]bool) []string {
	choices := append([]string{}, capabilities...)
	known := map[string]bool{}
	for _, cap := range choices {
		known[cap] = true
	}
	unknown := []string{}
	for cap := range chosen {
		if !known[cap] {
			unknown = append(unknown, cap)
		}
	}
	sort.Strings(unknown)
	choices = append(choices, unknown...)
	return choices
}

var capabilities = []string{"text", "vision", "video", "file", "audio", "tool_calling", "streaming", "reasoning"}

func (a *App) saveEditor() {
	if a.editorKind == "connection" {
		a.saveConnection(true)
		return
	}
	endpoint := "/models/" + url.PathEscape(a.editorID)
	body := map[string]any{"capabilities": sortedSelection(a.editorCaps), "input_modalities": split(a.editorModalities)}
	if a.editorKind == "route" {
		endpoint = "/routes/" + url.PathEscape(a.editorID)
		body = map[string]any{"providers": split(a.editorRoute)}
	}
	a.work("正在保存", 35*time.Second, func(ctx context.Context, client *connection.Client) (func(), error) {
		err := client.JSON(ctx, "PUT", endpoint, body, nil)
		return func() { a.editorOpen = false; a.reload(); a.notice = "配置已保存" }, err
	})
}

var okGreen = ui.Hex("#21835a")

func formatLatency(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1f s", ms/1000)
	}
	return fmt.Sprintf("%.0f ms", ms)
}

func statusColor(c *ui.Context, value string) ui.Color {
	if value == "online" || value == "active" || value == "closed" {
		return okGreen
	}
	if value == "offline" || value == "open" {
		return c.Theme().Danger
	}
	if value == "degraded" || value == "half_open" || value == "candidate" {
		return c.Theme().Accent
	}
	return c.Theme().TextMuted
}
func status(c *ui.Context, value string) {
	translations := map[string]string{"online": "在线", "offline": "离线", "unknown": "未探测", "degraded": "降级", "idle": "空闲", "closed": "正常", "open": "熔断", "half_open": "试探中", "active": "已生效", "candidate": "待确认", "retired": "已停用"}
	text := translations[value]
	if text == "" {
		text = value
	}
	chip(c, text, statusColor(c, value))
}
func (a *App) overviewView(c *ui.Context) {
	s := a.dashboard.Summary
	theme := c.Theme()
	metrics := []struct{ Title, Value string }{
		{"今日请求", shortNumber(int64(s.TodayRequests))},
		{"成功率", fmt.Sprintf("%.1f%%", s.SuccessRate)},
		{"活跃模型", fmt.Sprint(s.ActiveModels)},
		{"平均延迟", formatLatency(s.AvgLatency)},
	}
	ui.Row(c).Background(theme.Surface).Radius(12).Border(1, theme.Border).AlignItems(ui.Center).Children(func() {
		for i, metric := range metrics {
			if i > 0 {
				ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(theme.Border)
			}
			ui.Row(c.Key(metric.Title)).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).Padding(14, 18).Children(func() {
				muted(c, metric.Title)
				ui.Text(c, metric.Value).Bold().FontSize(17).SingleLine()
			})
		}
	})
	ui.Row(c).Gap(20).Children(func() {
		muted(c, "运行时间  "+s.Uptime)
		muted(c, "缓存命中  "+percent(s.CacheHitRate))
		muted(c, "缓存读取  "+shortNumber(s.CacheRead))
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "上游健康").FontSize(18).Bold()
		if count := len(a.dashboard.Providers); count > 0 {
			tally := map[string]int{}
			for _, p := range a.dashboard.Providers {
				switch {
				case p.Status == "online":
					tally["online"]++
				case p.Status == "offline":
					tally["offline"]++
				case p.Status == "degraded":
					tally["degraded"]++
				default:
					tally["unknown"]++
				}
			}
			if tally["online"] > 0 {
				chip(c, fmt.Sprintf("%d 在线", tally["online"]), okGreen)
			}
			if tally["degraded"] > 0 {
				chip(c, fmt.Sprintf("%d 降级", tally["degraded"]), theme.Accent)
			}
			if tally["offline"] > 0 {
				chip(c, fmt.Sprintf("%d 离线", tally["offline"]), theme.Danger)
			}
			if tally["unknown"] > 0 {
				chip(c, fmt.Sprintf("%d 未探测", tally["unknown"]), theme.TextMuted)
			}
		}
	})
	if len(a.dashboard.Providers) == 0 {
		empty(c, "网关暂未配置 Provider")
		return
	}
	ui.Table(c, nil, []ui.TableColumn{{Title: "Provider"}, {Title: "状态", Width: 110}, {Title: "请求", Width: 90}, {Title: "延迟", Width: 110}}, len(a.dashboard.Providers), func(row, col int) {
		p := a.dashboard.Providers[row]
		switch col {
		case 0:
			ui.Text(c, p.Name).Bold()
		case 1:
			status(c, p.Status)
		case 2:
			ui.Text(c, fmt.Sprint(p.Requests))
		case 3:
			ui.Text(c, formatLatency(p.AvgLatency))
		}
	}).Grow(1)
}
func (a *App) modelsView(c *ui.Context) {
	ui.TextInput(c, &a.modelFilter).Placeholder("搜索模型或 Provider").Label("搜索模型")
	filtered, changed := a.listViews.models.apply(a.models, a.modelFilter, modelSearchText)
	if changed {
		a.listViews.modelList = ui.ListState{}
	}
	if filtered.Len() == 0 {
		empty(c, "没有匹配的模型")
		return
	}
	ui.List(c, &a.listViews.modelList, filtered.Len(), func(i int) {
		m := filtered.At(i)
		ui.Column(c.Key(m.Name)).Children(func() {
			row(c, func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, m.Name).FontSize(18).Bold().Selectable()
						muted(c, m.Provider+" · "+strings.Join(m.Capabilities, " / "))
					})
					status(c, m.Status)
					ui.Button(c, "能力设置").Label(m.Name + " 能力设置").Disabled(a.busy != "").OnClick(func() {
						a.editorKind = "model"
						a.editorID = m.Name
						a.editorCaps = map[string]bool{}
						for _, cap := range m.Capabilities {
							a.editorCaps[cap] = true
						}
						a.editorModalities = strings.Join(m.Modalities, ", ")
						a.editorOpen = true
					})
					ui.Button(c, "路由顺序").Label(m.Name + " 路由顺序").Disabled(a.busy != "").OnClick(func() {
						a.editorKind = "route"
						a.editorID = m.Name
						names := append([]string{}, m.Providers...)
						for _, r := range a.routes {
							if r.Model == m.Name {
								names = nil
								for _, p := range r.Providers {
									names = append(names, p.Name)
								}
							}
						}
						a.editorRoute = strings.Join(names, ", ")
						a.editorOpen = true
					})
				})
				meta(c, "请求 "+shortNumber(int64(m.Requests)), "Tokens "+shortNumber(m.Tokens), "延迟 "+formatLatency(m.AvgLatency), "缓存命中 "+percent(m.CacheHitRate))
			})
		})
	}).Grow(1)
}
func (a *App) providersView(c *ui.Context) {
	if len(a.providers) == 0 {
		empty(c, "没有 Provider")
		return
	}
	ui.List(c, nil, len(a.providers), func(i int) {
		p := a.providers[i]
		ui.Column(c.Key(p.Name)).Children(func() {
			row(c, func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Text(c, p.Name).FontSize(18).Bold().Grow(1)
					status(c, p.Status)
					status(c, p.State)
					enabled := p.Enabled == nil || *p.Enabled
					if ui.Checkbox(c, &enabled, "启用").Label(p.Name + " 启用").Disabled(a.busy != "").Changed() {
						a.mutate("正在更新 Provider", "PATCH", "/providers/"+url.PathEscape(p.Name), map[string]any{"enabled": enabled})
					}
					ui.Button(c, "探测").Label("探测 " + p.Name).Disabled(a.busy != "").OnClick(func() {
						a.mutate("正在探测上游", "POST", "/providers/"+url.PathEscape(p.Name)+"/health-check", nil)
					})
					ui.Button(c, "重置熔断").Label("重置熔断 " + p.Name).Disabled(a.busy != "").OnClick(func() { a.mutate("正在重置熔断器", "POST", "/providers/"+url.PathEscape(p.Name)+"/reset", nil) })
				})
				meta(c, fmt.Sprintf("请求 %d", p.Requests), fmt.Sprintf("错误 %d", p.Errors), "延迟 "+formatLatency(p.AvgLatency))
				if p.HasProbe {
					ui.Text(c, "最近探测："+p.ProbeDetail).Selectable()
					muted(c, p.LastProbe)
				} else {
					muted(c, "尚未主动探测；状态仅反映熔断器。")
				}
				if p.NextRetry != "" {
					muted(c, "下次重试："+p.NextRetry)
				}
			})
		})
	}).Grow(1)
}
func (a *App) logsView(c *ui.Context) {
	ui.Row(c).Gap(12).Children(func() {
		ui.TextInput(c, &a.logFilter).Label("搜索日志").Placeholder("搜索模型、Provider、请求 ID 或错误").Grow(1)
		if ui.Select(c, &a.logLimit, []string{"50", "100", "200"}).Label("日志条数").Disabled(a.busy != "").Changed() {
			a.reload()
		}
	})
	filtered, changed := a.listViews.logs.apply(a.logs, a.logFilter, logSearchText)
	if changed {
		a.logSelected = -1
		a.table = ui.ListState{}
	}
	if filtered.Len() == 0 {
		empty(c, "暂无匹配的请求日志")
		return
	}
	a.table.Selected = &a.logSelected
	a.table.Key = func(i int) any {
		l := filtered.At(i)
		return l.RequestID + l.Timestamp + fmt.Sprint(i)
	}
	ui.Table(c, &a.table, []ui.TableColumn{{Title: "时间", Width: 170}, {Title: "模型"}, {Title: "Provider", Width: 150}, {Title: "状态", Width: 65}, {Title: "延迟", Width: 100}}, filtered.Len(), func(row, col int) {
		l := filtered.At(row)
		switch col {
		case 0:
			ui.Text(c, l.Timestamp).SingleLine()
		case 1:
			ui.Text(c, l.Model).SingleLine()
		case 2:
			ui.Text(c, l.Provider).SingleLine()
		case 3:
			ui.Text(c, fmt.Sprint(l.Code))
		case 4:
			ui.Text(c, formatLatency(l.Latency))
		}
	}).Grow(1).MinHeight(130)
	if a.logSelected >= 0 && a.logSelected < filtered.Len() {
		l := filtered.At(a.logSelected)
		ui.Scroll(c).MaxHeight(220).Gap(6).Padding(12).Background(c.Theme().Surface).Radius(8).Selectable().Children(func() {
			ui.Text(c, l.Method+" "+l.Path+" · "+l.RequestID).Bold()
			meta(c, "输入 "+shortNumber(l.Input), "输出 "+shortNumber(l.Output)+" tokens", "缓存读取 "+shortNumber(l.CacheRead), fmt.Sprintf("流式 %t", l.Stream))
			if l.Error != "" {
				ui.Text(c, l.Error).TextColor(c.Theme().Danger)
			}
			for _, p := range l.Attempts {
				ui.Row(c).Gap(10).Children(func() {
					ui.Text(c, p.Provider).SingleLine()
					muted(c, p.Status)
					muted(c, fmt.Sprintf("HTTP %d", p.Code))
					muted(c, formatLatency(p.Latency))
					if p.Error != "" {
						ui.Text(c, p.Error).TextColor(c.Theme().Danger).SingleLine()
					}
				})
			}
		})
	}
}
