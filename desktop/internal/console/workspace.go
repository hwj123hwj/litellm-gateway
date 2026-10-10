package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/hwj123hwj/litellm-gateway/desktop/internal/connection"
	"math"
	"sort"
	"strings"
	"time"
)

func (a *App) acceptSkills(d Skills) {
	a.skills = d
	a.targets = strings.Join(d.Targets, ", ")
	a.skillSelection = map[string]bool{}
	for _, id := range d.Enabled {
		a.skillSelection[id] = true
	}
	a.skillDirty = false
}
func sortedSelection(m map[string]bool) []string {
	ids := []string{}
	for id, v := range m {
		if v {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
func (a *App) skillsView(c *ui.Context) {
	ui.Row(c).Gap(10).Children(func() {
		ui.TextInput(c, &a.project).Grow(1).Label("项目路径").Placeholder("留空管理全局技能；填写网关主机上的项目绝对路径").Disabled(a.busy != "" || a.skillDirty)
		if a.localConnection() {
			ui.Button(c, "本机选择").Label("本机选择目录").Disabled(a.busy != "" || a.skillDirty).OnClick(a.pickLocalFolder)
		}
		ui.Button(c, "浏览").Label("浏览项目路径").Disabled(a.busy != "" || a.skillDirty).OnClick(a.openBrowser)
		ui.Button(c, "读取范围").Disabled(a.busy != "" || a.skillDirty).OnClick(func() { a.loadedProject = strings.TrimSpace(a.project); a.loaded = false; a.reload() })
	})
	if !a.skills.Configured {
		card(c, func() { ui.Text(c, "技能库尚未配置").Bold(); muted(c, a.skills.Hint) })
		return
	}
	ui.Row(c).Gap(12).Children(func() {
		if ui.TextInput(c, &a.targets).Grow(1).Label("技能安装目录").Disabled(a.busy != "" || a.loadedProject != "").Changed() {
			a.skillDirty = true
		}
		ui.Textf(c, "%d 个已选", len(sortedSelection(a.skillSelection)))
	})
	if a.loadedProject != "" {
		muted(c, "项目范围："+a.loadedProject)
	} else {
		muted(c, "全局范围 · 多个安装目录用逗号分隔")
	}
	ui.List(c, nil, len(a.skills.Skills), func(i int) {
		s := a.skills.Skills[i]
		ui.Column(c.Key(s.ID)).Children(func() {
			row(c, func() {
				name := s.Name
				if name == "" {
					name = s.ID
				}
				value := a.skillSelection[s.ID]
				ui.Row(c).Gap(12).Children(func() {
					if ui.Checkbox(c, &value, name).Grow(1).Disabled(a.busy != "").Changed() {
						a.skillSelection[s.ID] = value
						a.skillDirty = true
					}
					installed := len(a.skills.Targets) > 0
					for _, target := range a.skills.Targets {
						installed = installed && s.Installed[target]
					}
					if installed {
						chip(c, "已安装", okGreen)
					} else {
						muted(c, "未安装")
					}
				})
				muted(c, s.Description)
			})
		})
	}).Grow(1).Children(func() {
		if len(a.skills.Skills) == 0 {
			muted(c, "技能库中暂无技能")
		}
	})
	if len(a.skills.Local) > 0 {
		muted(c, fmt.Sprintf("另有 %d 个本地自定义技能，同步时保留。", len(a.skills.Local)))
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			muted(c, a.skills.Manifest)
			if a.skillDirty {
				ui.Text(c, "有未保存的选择；切换安装范围前请保存或撤销。").TextColor(c.Theme().Accent)
			}
		})
		ui.Button(c, "撤销选择").Disabled(a.busy != "" || !a.skillDirty).OnClick(func() { a.acceptSkills(a.skills) })
		ui.Button(c, "仅保存清单").Disabled(a.busy != "" || !a.skillDirty || len(split(a.targets)) == 0).OnClick(func() { a.saveSkills(false) })
		ui.PrimaryButton(c, "保存并同步").Disabled(a.busy != "" || len(split(a.targets)) == 0).OnClick(func() { a.saveSkills(true) })
	})
}
func (a *App) saveSkills(sync bool) {
	body := map[string]any{"targets": split(a.targets), "enabled": sortedSelection(a.skillSelection)}
	query := skillsQuery(a.loadedProject)
	dirty := a.skillDirty
	a.work("正在更新技能", 60*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		var result Skills
		if dirty {
			if err := c.JSON(ctx, "PUT", "/skills/config"+query, body, &result); err != nil {
				return nil, err
			}
			if result.Error != "" {
				return nil, errors.New(result.Error)
			}
		}
		if sync {
			if err := c.JSON(ctx, "POST", "/skills/sync"+query, nil, &result); err != nil {
				return nil, err
			}
			if result.Error != "" {
				return nil, errors.New(result.Error)
			}
		}
		return func() {
			a.acceptSkills(result)
			a.notice = "技能清单已保存"
			if sync {
				a.notice = "技能同步完成"
				if result.Sync != nil {
					for _, report := range result.Sync.Targets {
						if len(report.Errors) > 0 {
							a.problem += report.Target + "：" + strings.Join(report.Errors, "；") + "\n"
						}
						if len(report.Skipped) > 0 {
							a.notice += "；保留 " + strings.Join(report.Skipped, ", ")
						}
					}
				}
			}
		}, nil
	})
}
func (a *App) memoryView(c *ui.Context) {
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, "状态筛选")
		if ui.Select(c, &a.memoryFilter, []string{"all", "candidate", "active", "retired"}).Label("记忆状态").Disabled(a.busy != "").Changed() {
			a.reload()
		}
		ui.Spacer(c)
		muted(c, fmt.Sprintf("最近 %d 条", len(a.memories)))
	})
	ui.List(c, nil, len(a.memories), func(i int) {
		m := a.memories[i]
		ui.Column(c.Key(fmt.Sprint(m.ID))).Children(func() {
			row(c, func() {
				ui.Row(c).Gap(12).Children(func() {
					status(c, m.Status)
					muted(c, m.Scope+" · "+m.ScopeKey)
					ui.Spacer(c)
					if m.Status == "candidate" {
						ui.Button(c, "确认生效").Label(fmt.Sprintf("确认记忆 %d", m.ID)).Disabled(a.busy != "").OnClick(func() { a.mutate("正在确认记忆", "POST", fmt.Sprintf("/memories/%d/confirm", m.ID), nil) })
					}
					if m.Status != "retired" {
						ui.Button(c, "停用").Label(fmt.Sprintf("停用记忆 %d", m.ID)).Disabled(a.busy != "").OnClick(func() { a.mutate("正在停用记忆", "POST", fmt.Sprintf("/memories/%d/retire", m.ID), nil) })
					}
					ui.Button(c, "删除").Label(fmt.Sprintf("删除记忆 %d", m.ID)).Disabled(a.busy != "").OnClick(func() {
						a.editorTitle = "永久删除这条记忆，无法撤销：\n" + m.Statement
						a.deleteAction = func() { a.mutate("正在删除记忆", "DELETE", fmt.Sprintf("/memories/%d", m.ID), nil) }
						a.deleteOpen = true
					})
				})
				ui.Text(c, m.Statement).Selectable()
				meta(c, "来源 "+m.Source, fmt.Sprintf("置信度 %.2f", m.Confidence), fmt.Sprintf("命中 %d 次", m.Hits), m.Updated)
			})
		})
	}).Grow(1).Children(func() {
		if len(a.memories) == 0 {
			muted(c, "没有此状态的记忆")
		}
	})
	card(c, func() {
		ui.Text(c, "添加记忆").Bold()
		ui.Row(c).Gap(10).Children(func() {
			ui.Select(c, &a.memoryScope, []string{"global", "client", "project"}).Label("记忆范围").Disabled(a.busy != "")
			ui.TextInput(c, &a.memoryKey).Label("范围键").Placeholder("客户端名称或项目路径").Grow(1).Disabled(a.busy != "" || a.memoryScope == "global")
		})
		ui.TextInput(c, &a.memoryText).Label("记忆内容").Placeholder("输入希望助理记住的事实").Disabled(a.busy != "")
		ui.PrimaryButton(c, "添加记忆").Disabled(a.busy != "" || strings.TrimSpace(a.memoryText) == "" || (a.memoryScope != "global" && strings.TrimSpace(a.memoryKey) == "")).OnClick(a.addMemory)
	})
}
func (a *App) addMemory() {
	key := strings.TrimSpace(a.memoryKey)
	if a.memoryScope == "global" {
		key = ""
	}
	body := map[string]any{"scope_type": a.memoryScope, "scope_key": key, "statement": strings.TrimSpace(a.memoryText), "confidence": 1}
	a.work("正在添加记忆", 35*time.Second, func(ctx context.Context, c *connection.Client) (func(), error) {
		err := c.JSON(ctx, "POST", "/memories", body, nil)
		return func() { a.memoryText = ""; a.reload(); a.notice = "记忆已添加" }, err
	})
}
func (a *App) settingsView(c *ui.Context) {
	ui.Scroll(c).Grow(1).Gap(16).Children(func() {
		card(c, func() {
			ui.Text(c, "助理人设").FontSize(18).Bold()
			muted(c, "留空使用默认提示词。配置保存在当前连接的网关主机。")
			ui.TextArea(c, &a.promptDraft).Height(150).Label("自定义助理提示词").Placeholder(a.prompt.Default).Disabled(a.busy != "")
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "恢复默认").Disabled(a.busy != "").OnClick(func() { a.promptDraft = "" })
				ui.PrimaryButton(c, "保存人设").Disabled(a.busy != "").OnClick(func() {
					draft := a.promptDraft
					a.work("正在保存人设", 35*time.Second, func(ctx context.Context, client *connection.Client) (func(), error) {
						var p Prompt
						err := client.JSON(ctx, "PUT", "/assistant/prompt", map[string]string{"prompt": draft}, &p)
						return func() { a.prompt = p; a.notice = "人设已保存" }, err
					})
				})
			})
		})
		ui.Text(c, "同步模型到客户端").FontSize(18).Bold()
		muted(c, "写入当前网关主机上的客户端配置；其他客户端配置按服务端规则保留。")
		for _, target := range []string{"pi", "zcode", "harness"} {
			cfg := a.configs[target]
			choices := a.configSelection[target]
			ui.Column(c.Key(target)).Children(func() {
				card(c, func() {
					ui.Row(c).Children(func() {
						ui.Text(c, strings.ToUpper(target)).FontSize(18).Bold()
						ui.Spacer(c)
						if configInSync(cfg, choices) {
							chip(c, "已同步", okGreen)
						} else {
							chip(c, "待同步", c.Theme().Accent)
						}
					})
					if cfg.Error != "" {
						ui.Text(c, cfg.Error).TextColor(c.Theme().Danger)
						return
					}
					ui.Text(c, cfg.Path).Selectable().TextColor(c.Theme().TextMuted)
					ui.Row(c).Wrap().Gap(12).Children(func() {
						for _, m := range cfg.Desired {
							value := choices[m.ID]
							if ui.Checkbox(c.Key(m.ID), &value, m.ID).Disabled(a.busy != "").Changed() {
								choices[m.ID] = value
							}
						}
					})
					removed := []string{}
					for _, id := range cfg.Current {
						if !choices[id] {
							removed = append(removed, id)
						}
					}
					if len(removed) > 0 {
						muted(c, "同步后移除："+strings.Join(removed, ", "))
					}
					if len(cfg.Skipped) > 0 {
						muted(c, "跳过缺失条目："+strings.Join(cfg.Skipped, ", "))
					}
					ui.Row(c).Gap(8).Children(func() {
						ui.Button(c, "全选").Label("全选 " + target).Disabled(a.busy != "").OnClick(func() {
							for _, m := range cfg.Desired {
								choices[m.ID] = true
							}
						})
						ui.Button(c, "清空").Label("清空 " + target).Disabled(a.busy != "").OnClick(func() {
							for id := range choices {
								choices[id] = false
							}
						})
						ui.PrimaryButton(c, "同步 "+target).Disabled(a.busy != "" || len(sortedSelection(choices)) == 0).OnClick(func() {
							ids := sortedSelection(choices)
							a.work("正在同步 "+target, 35*time.Second, func(ctx context.Context, client *connection.Client) (func(), error) {
								var result ClientConfig
								err := client.JSON(ctx, "POST", "/"+target+"/sync", map[string]any{"model_ids": ids}, &result)
								if err == nil && result.Error != "" {
									err = errors.New(result.Error)
								}
								return func() { a.configs[target] = result; a.notice = target + " 模型配置已同步" }, err
							})
						})
					})
				})
			})
		}
		card(c, func() {
			ui.Text(c, "最近的助理反馈").FontSize(18).Bold()
			if len(a.feedback) == 0 {
				muted(c, "暂无反馈")
			}
			for _, f := range a.feedback {
				ui.Text(c, f.Rating+" · "+f.Created).Bold()
				ui.Text(c, f.Excerpt).MaxLines(3).Selectable()
				if f.Note != "" {
					muted(c, f.Note)
				}
				ui.Divider(c)
			}
		})
	})
}
func bubble(c *ui.Context, who, text string, user bool) {
	theme := c.Theme()
	background := theme.Surface
	labelColor := theme.TextMuted
	if user {
		background = theme.Accent.Alpha(.08)
		labelColor = theme.Accent
	}
	ui.Column(c).Padding(14).Gap(6).Background(background).Border(1, theme.Border).Radius(10).Children(func() {
		ui.Text(c, who).Bold().FontSize(13).TextColor(labelColor)
		ui.Text(c, text).Selectable()
	})
}
func (a *App) assistantView(c *ui.Context) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		options := append([]string{chatDefaultOption}, a.chatModels...)
		if a.chatModel == "" {
			a.chatModel = chatDefaultOption
		}
		ui.Select(c, &a.chatModel, options).Label("模型").Disabled(a.busy != "")
		if a.chatDefault != "" {
			muted(c, "默认 "+a.chatDefault)
		}
		ui.Spacer(c)
		muted(c, "助理可用 gateway_status / gateway_logs 查询实时运行状态")
	})
	ui.Scroll(c).Grow(1).TrackScroll(&a.chatList).Gap(18).Selectable().Children(func() {
		if a.chatUser == "" {
			ui.Text(c, "你的网关助理").FontSize(22).Bold()
			muted(c, "可以询问模型、路由与运行状态，或整理工作区记忆。")
			ui.Row(c).Wrap().Gap(8).Children(func() {
				for _, suggestion := range []string{"今天网关运行得怎么样？", "哪些 Provider 熔断了？", "总结待确认的记忆"} {
					prompt := suggestion
					ui.Button(c, prompt).OnClick(func() { a.chatInput = prompt })
				}
			})
		}
		if a.chatUser != "" {
			bubble(c, "你", a.chatUser, true)
		}
		if a.chatReply != "" {
			bubble(c, "助理", a.chatReply, false)
		}
		if a.chatStatus != "" {
			muted(c, a.chatStatus)
		}
	})
	if a.chatReply != "" && a.busy == "" {
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &a.feedbackNote).Label("反馈备注").Placeholder("可选：反馈备注").Grow(1)
			for _, rating := range []string{"up", "down"} {
				label := "有帮助"
				if rating == "down" {
					label = "需改进"
				}
				ui.Button(c, label).OnClick(func() {
					a.mutate("正在提交反馈", "POST", "/assistant/feedback", map[string]string{"rating": rating, "reply": a.chatReply, "note": a.feedbackNote})
				})
			}
		})
	}
	ui.TextArea(c, &a.chatInput).Height(88).Label("助理消息").Placeholder("输入消息；点击发送").Disabled(a.busy != "")
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		ui.Button(c, "清空对话").Disabled(a.busy != "").OnClick(func() { a.chatUser = ""; a.chatReply = ""; a.chatStatus = "" })
		ui.PrimaryButton(c, "发送").Disabled(a.busy != "" || strings.TrimSpace(a.chatInput) == "").OnClick(a.sendChat)
	})
}
func (a *App) sendChat() {
	message := strings.TrimSpace(a.chatInput)
	if message == "" {
		return
	}
	a.chatUser = message
	a.chatInput = ""
	a.chatReply = ""
	a.chatStatus = "正在回复"
	a.chatList.Y = math.MaxFloat32
	seq := a.sequence + 1
	dispatch := a.dispatch
	a.work("助理回复中", 10*time.Minute, func(ctx context.Context, c *connection.Client) (func(), error) {
		done := false
		var streamError error
		size := 0
		body := map[string]string{"message": message}
		if model := a.chatModelParam(); model != "" {
			body["model"] = model
		}
		err := c.Stream(ctx, "/assistant/chat", body, func(data []byte) bool {
			var e Event
			if json.Unmarshal(data, &e) != nil {
				return true
			}
			size += len(e.Content)
			if size > 8<<20 {
				return false
			}
			dispatch(func() {
				if seq != a.sequence {
					return
				}
				follow := a.chatList.Y >= a.chatList.MaxY
				switch e.Type {
				case "text_delta":
					a.chatReply += e.Content
				case "tool_start":
					a.chatStatus = "正在调用 " + e.Tool
				case "tool_end":
					a.chatStatus = e.Tool + " 已完成"
				case "error":
					a.problem = e.Content
				case "done":
					if e.Content != "" {
						a.chatReply = e.Content
					}
					a.chatStatus = "回复完成"
				}
				if follow {
					a.chatList.Y = math.MaxFloat32
				}
			})
			if e.Type == "error" {
				streamError = errors.New("助理请求失败：" + e.Content)
			}
			if e.Type == "done" || e.Type == "error" {
				done = true
				return false
			}
			return true
		})
		if err == nil && streamError != nil {
			err = streamError
		}
		if err == nil && !done {
			err = errors.New("助理流式回复未完成，请重试")
		}
		return func() { a.chatStatus = "回复完成" }, err
	})
}
