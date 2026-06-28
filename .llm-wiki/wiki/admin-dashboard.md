# Admin Dashboard

## 概述

LiteLLM Gateway 管理面板，提供实时监控、模型管理、提供商状态、请求日志等功能。

## 技术栈

| 组件 | 技术 | 版本 |
|------|------|------|
| 前端框架 | React | 19.0 |
| 构建工具 | Vite | 6.0 |
| 状态管理 | Zustand | 5.0 |
| 移动端 | Capacitor | 7.0 |
| 语言 | TypeScript | 5.6 |
| CSS | 自定义 CSS（无 Tailwind） | — |

与 pi-go 项目技术栈完全一致。

## 项目结构

```
web/
├── capacitor.config.json    # Capacitor 配置
├── package.json             # 依赖配置
├── vite.config.ts           # Vite 配置（代理 /admin → localhost:4001）
├── index.html               # 入口 HTML
└── src/
    ├── api/
    │   ├── types.ts         # TypeScript 类型定义
    │   ├── client.ts        # API 客户端
    │   └── index.ts         # 导出
    ├── components/
    │   ├── Sidebar.tsx      # 侧边栏导航
    │   ├── Header.tsx       # 顶部 header
    │   ├── MobileTabBar.tsx # 移动端底部 Tab
    │   └── PageHeader.tsx   # 页面标题组件
    ├── hooks/
    │   ├── useFetch.ts      # 数据请求 hook（支持自动刷新）
    │   └── index.ts
    ├── pages/
    │   ├── Dashboard.tsx    # 仪表盘（KPI + 提供商 + 模型 + 图表）
    │   ├── Models.tsx       # 模型管理
    │   ├── Providers.tsx    # 提供商状态
    │   ├── Logs.tsx         # 请求日志
    │   └── Settings.tsx     # 设置
    ├── styles/
    │   └── app.css          # 响应式 CSS
    ├── App.tsx              # 根组件
    ├── main.tsx             # 入口
    └── vite-env.d.ts        # Vite 类型声明
```

## 后端 Admin API

| 端点 | 说明 |
|------|------|
| `GET /admin/dashboard` | KPI 概览 + 提供商状态 + 模型排行 |
| `GET /admin/providers` | 所有提供商详情 |
| `GET /admin/models` | 所有模型统计 |
| `GET /admin/logs?limit=100` | 最近请求日志 |
| `GET /admin/health` | 系统健康状态 |

需要 `Authorization: Bearer <LITELLM_MASTER_KEY>` 头。

## 使用方式

```bash
# 开发模式
cd web && npm run dev

# 构建 Web 版本
npm run build

# 初始化 Android 项目（首次）
npm run android:init

# 打包 Android Debug APK
npm run android:debug

# 打包 Android Release APK
npm run android:release
```

## 响应式布局

- **桌面端（≥1024px）**：左侧固定侧边栏 + 4 列 KPI + 6 列提供商网格
- **移动端（<1024px）**：底部 Tab 导航 + 2 列 KPI + 横向滚动提供商

## 数据流

1. 前端通过 `useFetch` hook 调用 `/admin/*` API
2. Go gateway 的 metrics collector 从内存中聚合指标
3. 每次 API 请求自动采集：模型、提供商、延迟、token 用量
4. 支持自动刷新（Dashboard 10s，Logs 10s）
