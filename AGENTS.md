# 项目准则

## 1. 项目定位

LLM Gateway 是个人 AI 基础设施，不只是 API 转发器。它为 HwjCode、Claude Code、Codex CLI、Pi、飞书及其他 AI 应用提供：

- 统一 API 入口和认证；
- OpenAI Chat Completions、Responses、Anthropic Messages 兼容；
- 模型能力路由、多 Provider fallback、健康检查和熔断；
- Provider、模型、路由、日志和运行状态管理；
- Web Dashboard、React Native Android App 和跨平台部署；
- 可选的对话归档与知识库导出。

需求范围以 [PRD.md](PRD.md) 为准，API 和部署细节以 [go-gateway/README.md](go-gateway/README.md) 与 [docs/](docs/) 为准。

## 2. 仓库结构

| 目录 | 作用 |
|---|---|
| go-gateway/ | Go 网关运行时、Provider、路由、认证、指标和 Admin API |
| web/ | React + Vite Web Dashboard |
| mobile-app/ | React Native + Expo Android 管理端 |
| scripts/ | 安装、Codex 配置和回归测试脚本 |
| docs/ | 详细设计、兼容性和部署文档 |
| PRD.md | 产品需求和范围边界 |

不要新增或恢复 desktop/、旧 Capacitor Android 工程或静态 mockup 作为正式客户端。

## 3. 配置和事实来源

- Provider、模型别名、模型能力和路由链以 go-gateway/providers.yaml 为主要配置来源；
- 运行时可用模型以 GET /v1/models 为准；
- API Key、OAuth Token 和 Admin Token 只能来自环境变量或受保护配置；
- 不要在客户端、README、PRD、日志或测试数据中硬编码 Provider 密钥；
- 新增 Provider 或环境变量时，同时更新 providers.yaml、go-gateway/.env.example 和相关文档；
- 文档不要手工维护一份容易过期的 Provider/模型清单，优先引用配置和模型目录。

## 4. 网关开发准则

### API 与路由

- 保持三种协议的公开契约稳定；
- 保留工具调用、推理、扩展字段、多模态内容和流式语义；
- 图片、视频和文件请求必须先按 capabilities / input_modalities 选择兼容模型；
- 不要把图片请求静默降级到纯文本模型；
- fallback 前区分可重试错误与参数、认证、能力不匹配等不可重试错误；
- 每个逻辑请求保留 request ID，并能关联最终 Provider 和 Provider 尝试。

### Provider

- 新增 OpenAI 兼容 Provider 参考 internal/provider/openai.go；
- 新增 Anthropic 兼容 Provider 参考 internal/provider/anthropic.go；
- Provider API Key 不进入响应、日志或 Admin API；
- 运行时控制优先通过 Router、Admin API 和配置完成，避免把 Provider 判断散落在客户端。

### 指标与归档

- request_logs 只保存轻量指标，不直接存放大段 request/response 正文；
- 完整对话归档必须是独立、可开启、可脱敏、可清理的能力；
- 流式请求需要记录终态或中断原因；
- Authorization、Cookie、Token、密码和 API Key 永不落库；
- 多媒体原文默认保存元数据或引用，避免无限增长的 Base64/二进制正文。

## 5. 修改后的验证

按改动范围执行最小且充分的检查，不要只依赖编译成功：

### Go

~~~bash
cd go-gateway
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go build -o gateway .
~~~

生成的二进制、临时数据库和本地配置不要提交。

### Web

~~~bash
cd web
npm ci
npm run build
~~~

### Android

遵循 [mobile-app/AGENTS.md](mobile-app/AGENTS.md) 和 Expo v56 文档。修改 Android 客户端后至少执行依赖安装、TypeScript 检查和对应的 Expo/Android 构建验证。

### 脚本和安装器

修改 scripts/ 或安装流程后，执行对应的 Unix/Windows 回归测试；不要把平台下载缓存或构建产物提交到仓库。

## 6. 文档维护

- PRD.md 只描述产品需求、范围、核心验收和交付顺序；
- README.md 描述用户可见的安装、配置和常用使用方式；
- go-gateway/README.md 描述 API、Provider 配置和运行细节；
- docs/ 保存较长的技术方案、兼容性说明和排障记录；
- API、模型能力、Provider、部署流程发生变化时，更新对应文档；
- 文档中的模型和 Provider 不能与 providers.yaml 或 /v1/models 长期冲突。

## 7. Git、PR 与 CI

- 从最新的 main 创建 codex/ 前缀分支；
- 提交前检查工作树，避免把 .env、密钥、数据库、二进制和用户本地文件提交；
- 通过 Pull Request 合并，不直接向 main 推送；
- 代码修改完成后检查相关 GitHub Actions 状态；
- CI 失败必须定位并修复，不能留下已知红灯；
- 纯文档修改至少执行 git diff --check，并确认链接、命令和当前代码状态一致。

常用检查：

~~~bash
git status --short --branch
git diff --check
gh run list --limit 3
gh run view <run-id> --log-failed
~~~

## 8. 常见操作

~~~bash
# 本地启动网关
cd go-gateway
go build -o gateway .
./gateway

# 配置 Pi
./gateway setup pi --dry-run

# Codex CLI 模型切换
codex-model gateway <model>
codex-model native
codex-model status
~~~

Codex 可用模型以网关 GET /v1/models 返回结果为准，不在本文件维护固定模型列表。
