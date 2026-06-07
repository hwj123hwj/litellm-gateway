# PRD: Claude Code 多模型网关

## 背景

目前使用多个 AI 模型提供商（智谱、小米、美团、EasyClaw、APIFree、OpenRouter 等），它们提供 OpenAI 和/或 Anthropic 兼容 API。

**痛点**：Claude Code 同一时间只能配置一个提供商，导致其他家的模型和额度闲置。

## 目标

搭建一个网关服务，让 Claude Code 通过一个入口访问所有提供商：

1. 智谱主力，自动 fallback 到小米/美团
2. 手动 `/model` 切换到任意提供商
3. 充分利用所有已有资源
4. 支持本地部署和服务器部署两种方式

## 模型提供商

| 提供商 | API 类型 | 模型 | 角色 |
|--------|----------|------|------|
| 智谱 GLM | OpenAI / Anthropic | glm-4.7, glm-5-turbo, glm-5.1, glm-4.7-flash | 主力（优先） |
| 小米 MiMo | OpenAI / Anthropic | mimo-v2.5, mimo-v2.5-pro | 备用 1 |
| 美团 LongCat | OpenAI | LongCat-Flash-Chat, LongCat-2.0-Preview | 备用 2 |
| EasyClaw | OpenAI | claude-sonnet-4-6, claude-opus-4-6 | 真实 Claude |
| APIFree | OpenAI | skywork-ai/skyclaw-v1, skyclaw-v1-lite | SkyClaw Agent |
| OpenRouter | OpenAI | 免费模型 + GPT-5.5 | 免费兜底 |
| ChatGPT Codex | Responses API | gpt-5.5, gpt-5.5-pro, o4-mini | OAuth 直连 |
| GitHub Copilot | OpenAI | Gemini/GPT 模型 | 免费教育套餐 |
| DeepV Server | 内部 | deepseek-flash, glm-5 等 | 内部聚合 |

### 模型分级映射

| Claude Code 层级 | 智谱 | 小米 | 美团 |
|------------------|------|------|------|
| Haiku（快速） | glm-haiku (glm-4.7) | — | — |
| Sonnet（主力） | glm-sonnet (glm-5-turbo) | mimo-sonnet (mimo-v2.5) | longcat-sonnet |
| Opus（强力） | glm-opus (glm-5.1) | mimo-opus (mimo-v2.5-pro) | longcat-opus |

## 架构

```
Claude Code / Codex CLI ──▶ 网关 (:4001)
                              ├── /v1/chat/completions  (OpenAI)
                              ├── /v1/messages          (Anthropic)
                              ├── /v1/responses         (Responses API)
                              │
                              ├── 智谱 GLM (OpenAI/Anthropic)
                              ├── 小米 MiMo (Anthropic) ── fallback
                              ├── 美团 LongCat (OpenAI) ── fallback
                              ├── EasyClaw (OpenAI)
                              ├── APIFree (OpenAI)
                              ├── OpenRouter (OpenAI)
                              ├── ChatGPT Codex (Responses API, OAuth)
                              ├── GitHub Copilot (OpenAI)
                              └── DeepV Server (内部)
```

### 部署方式

| 方式 | 适用场景 | 说明 |
|------|----------|------|
| 本地运行 | 仅本机使用，最简单 | `go build -o gateway . && ./gateway` |
| GitHub Actions | 自动构建部署到服务器 | 推送 `main` 分支自动触发，详见 [go-gateway/README.md](go-gateway/README.md) |
| 手动服务器部署 | 自定义部署 | 详见 [go-gateway/README.md](go-gateway/README.md) |

## Claude Code 客户端配置

### settings.json

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-gateway-key"
  }
}
```

服务器部署时将 `ANTHROPIC_BASE_URL` 改为 `https://your-domain.com/v1`。

### 模型切换

在 Claude Code 中通过 `/model` 命令手动切换：

| 命令 | 效果 | 说明 |
|------|------|------|
| `/model coding` | 自动 fallback 组 | 智谱优先，挂了切小米→美团 |
| `/model coding-anthropic` | Anthropic 风格 fallback | 走 Anthropic API |
| `/model free` | OpenRouter 免费模型 | 零成本 |
| `/model glm-haiku` | 智谱 glm-4.7 | 快速任务 |
| `/model glm-sonnet` | 智谱 glm-5-turbo | 日常编码（推荐） |
| `/model glm-opus` | 智谱 glm-5.1 | 复杂推理 |
| `/model glm-flash` | 智谱 glm-4.7-flash | 免费模型 |
| `/model mimo-sonnet` | 小米 mimo-v2.5 | 小米主力 |
| `/model mimo-opus` | 小米 mimo-v2.5-pro | 小米强力 |
| `/model longcat-sonnet` | 美团 LongCat | 长上下文 |
| `/model easyclaw-sonnet` | EasyClaw Claude Sonnet | 真实 Claude |
| `/model easyclaw-opus` | EasyClaw Claude Opus | 真实 Claude 旗舰 |
| `/model sky-opus` | APIFree SkyClaw | SkyClaw Agent |
| `/model gpt-5.5` | ChatGPT Codex | 需 HTTP_PROXY |

## 验收标准

- [x] 网关启动正常，Claude Code 通过网关完成一次完整的编码任务
- [x] `coding` 组 fallback 正常（关闭智谱后自动切小米/美团）
- [x] `/model glm-sonnet`、`/model mimo-sonnet`、`/model longcat-sonnet` 手动切换正常
- [x] `.env` 不在 git 追踪中
- [x] 服务器部署时：无认证 key 的请求被拒绝
