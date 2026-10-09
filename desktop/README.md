# Gateway 桌面客户端

MyGo 提供原生窗口、系统 WebView、菜单和托盘；界面复用 `../web` 的 React 管理页。客户端通过已有 Admin API 连接本地或远程网关，不安装、启动或替换网关服务。

## 开发与构建

需要 Go 1.27.1+、Node.js/npm。MyGo 固定到 `go.mod` 中的版本，不使用浮动 latest。

```bash
npm --prefix ../web ci
go test -race ./...
go vet ./...
go run github.com/egoist/mygo/cmd/mygo build
```

产物位于 `build.noindex/<os>-<arch>/`。生成的前端、应用包和开发缓存不提交，也不放进普通项目目录供 Spotlight 重复索引。

本机开发：

```bash
npm --prefix ../web run build:desktop
go run github.com/egoist/mygo/cmd/mygo dev
```

跨平台编译可使用 `build -platform darwin/arm64,darwin/amd64,windows/amd64,linux/amd64`。macOS 打包需要 macOS；Windows Web UI 需要 WebView2，Linux 需要 GTK3 和 WebKitGTK4.1。首版实机验收平台为 macOS arm64，其他平台需另做实机验收。

## 连接

首次启动填写连接名称、网关根地址与 `ADMIN_TOKEN` 或 `LITELLM_MASTER_KEY`。地址也支持带 `/admin` 后缀和反向代理前缀。先通过 `/admin/health` 验证，再保存。可添加、编辑、移除或切换多个连接；编辑时 Token 留空会保留已有值。切换重新加载页面，清除旧主机的缓存和请求。

连接配置位于 MyGo 的应用用户数据目录中的 `connections.json`：目录创建权限为 `0700`，文件为 `0600`。Token 不返回 renderer，不存 localStorage；文件本身不是加密保险库。远程连接建议 HTTPS；TLS 证书照常验证。主进程不跟随上游重定向，也不把上游 Cookie 返回页面。代理只接受客户端 Origin 的 `/admin/` 请求，支持助理 SSE 流式输出与取消。

关闭窗口后应用留在托盘；点击 Dock 图标或托盘“打开 Gateway”恢复窗口。`⌘/Ctrl+,` 管理连接，`⌘/Ctrl+1` 显示窗口；完全退出使用应用菜单或托盘“退出”。窗口尺寸与位置会保存。

## macOS 安装与发布

本机安装只维护 `/Applications/Gateway.app` 一份，通过复制新产物替换此路径。不要每次构建都打开新的 `.app`，避免 Dock、Spotlight 和应用注册重复。MyGo 单实例锁避免同一客户端重复运行。

默认构建为 ad-hoc 签名，只适合本机开发。公开分发需在 `mygo.json` 配置 `macos.signingIdentity` 与 `macos.notarize.keychainProfile`，完成 Developer ID 签名、公证和票据附加；不要在仓库放证书或账号凭据。桌面客户端本身尚未配置在线自动更新。

故障复现时可用 `GATEWAY_DESKTOP_CONFIG_DIR` 指向临时目录，隔离测试配置。图标可通过 `swift scripts/icon.swift resources/icon.png` 重新生成。
