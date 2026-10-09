# 纯 Go 架构总览

> 状态：当前架构（2026-10）。运行代码全部为 Go，仓库不含 TypeScript、JavaScript、HTML/CSS、Node.js 构建链或 WebView 包装。
> 需求与范围见 [PRD.md](../PRD.md)，API 细节见 [go-gateway/README.md](../go-gateway/README.md)，构建与连接见 [desktop/README.md](../desktop/README.md)。

## 1. 系统组成

```text
Claude Code / Codex CLI / Pi / 飞书等客户端
   │  OpenAI Chat Completions · Responses · Anthropic Messages（:4001）
   ▼
┌─────────────────────────────┐        Admin API（独立 Token）        ┌──────────────────────────┐
│ go-gateway（Go 运行时）      │ ◀─────────────────────────────────── │ desktop（MyGo 原生客户端）│
│ 路由 · fallback · 熔断       │                                       │ 原生控件 UI，无 WebView    │
│ 指标 · 归档 · 记忆 · 助理     │                                       └──────────────────────────┘
└──────────┬──────────────────┘
           ▼
   GLM / Antigravity / DeepV / Copilot / ChatGPT OAuth 等上游 Provider
```

两个可执行单元：

- **go-gateway**：无状态要求低、单二进制 HTTP 服务，是数据面（三种协议转发）和管理面（Admin API）的唯一提供者；
- **desktop**：MyGo 原生桌面客户端，只通过 Admin API 管理网关，不启动、不替换、不内嵌网关服务。

Admin API 是两者之间唯一的契约边界：网关不感知客户端的存在，客户端也不读网关的本地文件或数据库。

## 2. go-gateway 内部结构

| 包 | 职责 |
|---|---|
| `config` | 加载 `providers.yaml`（Provider、模型、能力、fallback 链的唯一配置来源） |
| `provider` | 上游实现；OpenAI 兼容参考 `openai.go`，Anthropic 兼容参考 `anthropic.go` |
| `handlers` | 三种协议入口、模型目录、路由与 Admin API 的 HTTP 处理器 |
| `auth` | Bearer 认证（Master Key / Admin Token 分离） |
| `middleware` | request ID、指标采集、请求日志 |
| `requestmeta` | 请求关联元数据契约（request ID ↔ 最终 Provider 与每次尝试） |
| `metrics` | 轻量指标聚合（缓存统计、命中率、Provider 健康） |
| `storage` | SQLite 持久化（指标、归档、记忆、设置同文件分表） |
| `archive` | 对话归档：脱敏、终态记录、游标增量导出（`ARCHIVE_ENABLED` 独立开关） |
| `memory` | Agent 长期记忆：作用域路由、candidate/active 治理（见 [MEMORY_DESIGN.md](MEMORY_DESIGN.md)） |
| `assistant` | 常驻助理：EasyAgent SDK，LLM 调用回环走网关自身 |
| `skills` | custom-skills 技能市场的清单管理与符号链接同步 |
| `piconfig` / `zcodeconfig` / `dshconfig` | 把网关模型清单同步进 Pi / ZCode / deepseek-harness 的客户端配置 |

数据红线（AGENTS.md）：Provider API Key 不进响应、日志、Admin API；Authorization、Token、密码永不落库；归档默认脱敏且有保留期。

## 3. desktop 内部结构

| 文件/包 | 职责 |
|---|---|
| `main.go` | 窗口、托盘常驻、单实例锁、快捷键、窗口状态持久化 |
| `internal/connection` | 连接配置存储（`connections.json`，0600）与 Admin API 客户端（TLS 正常验证、不跟随重定向） |
| `internal/console` | 全部界面：连接管理、概览、模型与路由、Provider、日志、技能、记忆、助理、设置 |

界面约定：所有耗时操作走 `App.work/launch`（后台 goroutine + UI 线程回写 + 序列号防串台）；导航与切换主机会取消在途请求并清空旧主机数据；列表按可见行虚拟化。

## 4. TS → 纯 Go 迁移记录

浏览器 Dashboard（React/Vite + Node 构建链 + WebView 桌面壳）已于 2026-10-09 分三步移除，全部合入 `main`：

1. `5a44afe` — 新增 MyGo 原生桌面客户端（与 WebView 壳并存）；
2. `c613d60` — 桌面端从 WebView 切换为原生 Go UI；
3. `1b024f8` — 删除浏览器 Dashboard、前端工具链与嵌入静态资源。

防回退措施：

- `scripts/deploy-local-gateway.sh` 部署预检断言旧浏览器路由（`/`、`/dashboard`、`/assets/index.js` 等）返回 404、未认证返回 401；
- CI（`ci.yml`、`desktop.yml`）只安装 Go 工具链，不再有前端构建与 Dashboard 嵌入校验；
- AGENTS.md 明确不再新增 TypeScript/JavaScript/HTML/CSS/Node.js 构建链或 WebView 包装。

## 5. 部署形态

- 本机 macOS：`scripts/deploy-local-gateway.sh` 维护 LaunchAgent（隔离端口预检 → 原子切换 → 失败回滚）；
- 迷你主机 Linux：systemd 服务 + timer 每 10 分钟拉取 CI 发布的校验产物，健康检查失败自动回退；
- 服务器 / 其他设备：`scripts/install.sh|bat|ps1` 一键安装，版本标签触发 Release；
- 所有部署均为手动或半自动确认，推送 main 不触发部署。
