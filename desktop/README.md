# Gateway 原生桌面客户端

桌面界面使用 MyGo 的 `ui` 控件，以 Go 实现布局、表单、导航、虚拟列表和流式助理。窗口直接显示原生 UI，不启动 WebView，也不依赖 HTML、JavaScript、React 或 Node.js。客户端通过 Go 网络层直接调用网关 Admin API；仓库不再保留浏览器 Dashboard。客户端不安装、启动或替换网关服务。

## 开发与构建

需要 Go 1.27.1+。MyGo 固定到 `go.mod` 中的版本。

```bash
go test -race ./...
go run github.com/egoist/mygo/cmd/mygo vet
go run github.com/egoist/mygo/cmd/mygo build
```

本机开发使用 `go run github.com/egoist/mygo/cmd/mygo dev`。产物位于 `build.noindex/<os>-<arch>/`，构建缓存和应用包不提交。桌面 CI 仅使用 Go，不安装 Node.js 或构建浏览器界面。

支持 `build -platform darwin/arm64,darwin/amd64,windows/amd64,linux/amd64`。macOS 打包需要 macOS；原生控件由平台图形与文本系统绘制，不要求 WebView2/WebKitGTK。实机验收平台为 macOS arm64，其他平台需另做实机验收。

## 功能与连接

包含连接管理、运行概览、模型能力和路由顺序编辑、Provider 启停/探测/熔断重置、请求日志、技能清单与同步、记忆管理、流式助理、客户端模型同步和助理人设设置。列表按可见行构建，日志可搜索并查看单个请求的上游尝试。

首次启动填写连接名称、网关根地址与 `ADMIN_TOKEN` 或 `LITELLM_MASTER_KEY`。支持 `/admin` 后缀和反向代理前缀。先验证 `/admin/health`，再保存。可以添加、编辑、移除或切换多个连接；编辑时 Token 留空保留已有值。切换主机取消旧请求并清除旧主机页面数据；每个请求固定到发起时的连接。操作异步执行，可取消，不阻塞界面。

连接配置兼容原版桌面端，位于 MyGo 应用用户数据目录中的 `connections.json`。目录创建权限 `0700`，文件 `0600`，凭据仅由 Go 网络层使用；文件不是加密保险库。TLS 证书照常验证，不跟随带管理凭据的上游重定向。

技能的项目路径、安装目录和客户端同步路径属于**当前连接的网关主机**。全局和项目范围分别加载；未保存的技能选择必须保存或撤销后再切换范围。同步报告显示保留项和错误。助理支持增量回复、工具状态、停止和反馈；连接中断会显示提示。

关闭窗口后应用留在托盘；点击 Dock 或托盘恢复窗口。`⌘/Ctrl+,` 管理连接，`⌘/Ctrl+1` 显示窗口；应用菜单或托盘可完全退出。窗口尺寸与位置会保存。

## macOS 安装与发布

本机只维护 `/Applications/Gateway.app` 一份，通过替换这个固定路径更新。不要打开不同构建目录中的 `.app`，避免 Spotlight 和应用注册重复。构建目录使用 `.noindex` 后缀；单实例锁避免重复运行。

默认构建为 ad-hoc 签名。公开分发需在 `mygo.json` 配置 `macos.signingIdentity` 与 `macos.notarize.keychainProfile`，完成 Developer ID 签名、公证和票据附加；不在仓库保存证书或凭据。桌面客户端尚未配置在线自动更新。

复现问题时，可设置 `GATEWAY_DESKTOP_CONFIG_DIR` 到临时目录以隔离连接配置。应用图标保存在 `resources/icon.png`，由 MyGo 打包为各平台资源。
