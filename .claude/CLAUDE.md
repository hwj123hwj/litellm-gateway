# 项目准则

## 项目简介

LLM API 网关，支持多家提供商（GLM、MiMo、LongCat、EasyClaw、Copilot、ChatGPT）。技术细节见 [go-gateway/README.md](go-gateway/README.md)。

## Codex CLI 集成

网关支持 Codex CLI（`wire_api = "responses"`），通过 `/v1/responses` 端点提供 OpenAI Responses API 格式。

### 模型切换脚本

`~/.local/bin/codex-model` 用于快速切换 Codex 使用的模型：

```bash
codex-model gateway <model>  # 走网关（默认 glm-opus）
codex-model native           # 走 OpenAI 原生认证（GPT-5.5）
codex-model status           # 查看当前配置
```

网关可用模型：`glm-opus`, `glm-sonnet`, `glm-haiku`, `gpt-5.5`, `gpt-5.4-mini`, `longcat-sonnet`, `longcat-opus`, `coding`

所有模型统一走网关 `localhost:4001`，不需要 Codex 直连外部 API。

## 开发准则

### 代码修改后
1. **必须编译测试**：`cd go-gateway && go build -o gateway .`
2. **必须运行测试**：`go test ./...`
3. **提交前检查**：确保没有编译错误

### 提交与部署
1. **每次提交都会触发 CI/CD**：GitHub Actions 自动构建、测试、部署
2. **提交后必须检查 CI 状态**：`gh run list --limit 1`
3. **CI 失败时必须修复**：不能留红灯
4. **部署问题**：检查 `gh run view <id> --log-failed`

### 代码规范
- Go 代码遵循标准 go fmt
- 新增 provider 参考 `internal/provider/openai.go` 或 `anthropic.go`
- 新增环境变量需更新 `.env.example` 和文档
- 模型映射在 `router.go` 的 `mapModelName` 函数

### 文档维护
- 详细文档放在 `go-gateway/README.md` 或 `docs/` 目录
- CLAUDE.md 只写准则和引用，不重复详细内容
- 重要变更需更新 README.md

### 常见操作
```bash
# 本地启动
cd go-gateway && go build -o gateway . && ./gateway

# 后台启动
pkill -f "go-gateway/gateway"; ./gateway > /tmp/go-gateway.log 2>&1 &

# 查看日志
tail -f /tmp/go-gateway.log

# 检查 CI
gh run list --limit 3
```

## 提供商参考

| 提供商 | 类型 | 环境变量 |
|--------|------|----------|
| GLM | Anthropic/OpenAI | GLM_API_KEY |
| MiMo | Anthropic | MIMO_API_KEY |
| LongCat | OpenAI | LONGCAT_API_KEY |
| EasyClaw | OpenAI | EASYCLAW_API_KEY |
| Copilot | OpenAI | COPILOT_TOKEN + COPILOT_GITHUB_TOKEN |
| ChatGPT | Responses API (透传) | HTTP_PROXY + ~/.codex/auth.json |
| OpenRouter | OpenAI | OPENROUTER_API_KEY |
