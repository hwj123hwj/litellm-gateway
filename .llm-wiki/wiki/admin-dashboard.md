# Admin Dashboard

## 概述

LiteLLM Gateway 管理面板，提供实时监控、模型管理、提供商状态、请求日志等功能。

## 客户端边界

- 浏览器 Dashboard：`web/`（React + Vite），只负责 Web 访问
- Android 客户端：`mobile-app/`（React Native + Expo），唯一 Android 发布入口
- 旧的 Capacitor Android 工程已移至 `archive/legacy-capacitor-dashboard/`，仅供历史排查，不再构建或发布

## 技术栈

| 组件 | 技术 | 版本 |
|------|------|------|
| 前端框架 | React | 19.0 |
| 构建工具 | Vite | 6.0 |
| 状态管理 | Zustand | 5.0 |
| 语言 | TypeScript | 5.6 |
| CSS | 自定义 CSS（无 Tailwind） | — |

Android 客户端的技术栈和维护说明见 [`mobile-app/`](../../mobile-app/) 及
[`mobile-app-performance`](./mobile-app-performance.md)。

## 项目结构

```text
web/
├── package.json             # Web 依赖和脚本
├── vite.config.ts           # Vite 配置（代理 /admin → localhost:4001）
├── index.html               # 入口 HTML
└── src/
    ├── api/                 # API 类型和客户端
    ├── components/          # Sidebar、Header、响应式导航
    ├── hooks/               # 通用数据请求 hook
    ├── pages/               # Dashboard、Models、Providers、Logs、Settings
    ├── store/               # Zustand 全局状态
    ├── styles/              # 响应式 CSS
    ├── App.tsx              # 根组件
    └── main.tsx             # 入口
```

## 后端 Admin API

| 端点 | 说明 |
|------|------|
| `GET /admin/dashboard` | KPI 概览 + 提供商状态 + 模型排行 |
| `GET /admin/providers` | 所有提供商详情 |
| `GET /admin/models` | 所有模型统计 |
| `GET /admin/logs?limit=100` | 最近请求日志 |
| `GET /admin/health` | 系统健康状态 |
| `GET /admin/config` | 当前配置（providers、chains） |
| `GET /admin/stats` | 详细统计（models、providers、total_tokens） |

### 认证

- 优先使用 `ADMIN_TOKEN` 环境变量
- 后备使用 `LITELLM_MASTER_KEY`
- 请求头：`Authorization: Bearer <token>`

## 使用方式

```bash
# 开发模式
cd web && npm install && npm run dev

# 构建 Web 版本
npm run build
```

Android APK 由 `.github/workflows/android-apk.yml` 从 `mobile-app/` 构建和发布；不要在
`web/` 下运行 Android 打包命令。

## 配置

### 后端地址

在 Settings 页面可以配置 Gateway 地址：

- 开发模式：留空使用 Vite proxy（localhost:4001）
- 浏览器访问远程网关：输入 Gateway 的实际地址（如 `http://192.168.1.100:4001`）
- Android 模式：在 `mobile-app` 的设置页输入 Gateway 的实际地址

### API Key

在 Settings 页面输入 `LITELLM_MASTER_KEY` 或 `ADMIN_TOKEN`。

## 响应式布局

- **桌面端（≥1024px）**：左侧固定侧边栏 + 4 列 KPI + 6 列提供商网格
- **移动端（<1024px）**：底部 Tab 导航 + 2 列 KPI + 横向滚动提供商

## 数据流

1. 前端通过 Zustand store 调用 `/admin/*` API
2. Go gateway 的 metrics collector 从内存中聚合指标
3. SQLite 持久化存储请求日志和每日统计
4. 每次 API 请求自动采集：模型、提供商、延迟、token 用量
5. 支持自动刷新（Dashboard 10s，Logs 10s）

## 持久化

- SQLite 数据库：`{PI_GO_DATA_DIR}/metrics.db`
- 自动清理：每天凌晨 3 点清理 30 天前的数据
- WAL 模式：提高并发写入性能

## 维护边界

网页 Dashboard 与 Android 客户端分开维护，但只有 `mobile-app/` 可以产生 Android 发布包。
这样既保留浏览器端的 React + Vite 体验，也避免两套 Android 客户端产生安装包和升级混乱。
