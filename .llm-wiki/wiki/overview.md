---
type: concept
date: 2026-06-14
tags:
  - overview
  - architecture
  - gateway
---

# Project Overview: LLM Gateway

## Summary

A lightweight Go-based LLM API gateway that provides a unified entry point for multiple AI model providers. Built to solve the problem of Claude Code (or any AI coding agent) only supporting one provider at a time.

## Architecture

```
AI Agent (Codex/Claude Code) ──▶ Gateway (:4001)
                                   ├── /v1/chat/completions  (OpenAI)
                                   ├── /v1/messages          (Anthropic)
                                   ├── /v1/responses         (Responses API)
                                   │
                                   ├── Providers (via providers.yaml)
                                   └── Fallback chains
```

## Supported Providers

| Provider | Type | Role |
|----------|------|------|
| [[glm-provider\|智谱 GLM]] | OpenAI | Primary (opus/sonnet/haiku) |
| MiMo | Anthropic | Fallback |
| LongCat | OpenAI | Fallback |
| EasyClaw | OpenAI | Claude proxy |
| APIFree | OpenAI | SkyClaw Agent |
| OpenRouter | OpenAI | GPT + free models |
| DeepV Server | Internal | Aggregated |
| GitHub Copilot | OpenAI | Free tier |
| ChatGPT Codex | Responses API | OAuth direct |

## Key Design Decisions

- **Config-driven**: Add providers without code changes via `providers.yaml`
- **Fallback chains**: Primary → secondary → free fallback
- **Multi-protocol**: Supports OpenAI, Anthropic, and Responses API formats
- **Model aliases**: opus/sonnet/haiku naming for consistent tier mapping
- **Auth**: Single master key (`LITELLM_MASTER_KEY`) protects the gateway

## File Layout

```
go-gateway/
├── main.go              # Entry point, provider setup
├── providers.yaml       # Provider/model config (no-code changes needed)
├── .env                 # API keys and config
├── internal/
│   ├── config/          # Config loading (port, proxy, etc.)
│   ├── auth/            # Bearer token auth
│   ├── handlers/        # HTTP handlers (chat, models, health)
│   ├── middleware/       # Logging middleware
│   └── provider/        # Provider implementations + router
```

## Deployment

Runs as a macOS launchd service with auto-restart:
```bash
launchctl load ~/Library/LaunchAgents/local.go-gateway.plist
```
