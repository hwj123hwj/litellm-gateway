---
type: concept
date: 2026-06-21
tags:
  - overview
  - architecture
  - gateway
  - updated
---

# Project Overview: LLM Gateway (Updated 2026-06-21)

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
| [[glm-provider\|智谱 GLM]] | openai | Primary (opus/sonnet/haiku) |
| [[mimo-provider\|小米 MiMo]] | openai | Fallback 1 |
| [[longcat-provider\|美团 LongCat]] | openai | Fallback 2 (opus only) |
| EasyClaw | openai | Claude proxy |
| DeepV Server | custom | Internal aggregated |
| GitHub Copilot | custom | Free tier (Gemini/GPT) |
| ChatGPT Codex | responses | OAuth direct (proxy required) |

**Removed providers** (see [[source-codebase-2026-06-21]]):
- ~~OpenRouter~~ — removed 2026-06-21 (no longer used)
- ~~APIFree (SkyClaw)~~ — removed 2026-06-19 (balance exhausted)

## Key Design Decisions

- **Config-driven**: Add providers without code changes via `providers.yaml`
- **Fallback chains**: Primary → secondary (no more free-tier fallback)
- **Multi-protocol**: Supports OpenAI, Anthropic, and Responses API formats
- **Model aliases**: opus/sonnet/haiku naming for consistent tier mapping
- **Alias-only exposure**: Models with aliases only expose the alias in `/v1/models` (raw ID hidden)
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
│   ├── handlers/        # HTTP handlers (chat, models, health, admin)
│   ├── middleware/       # Logging + metrics middleware
│   ├── metrics/         # In-memory metrics collector
│   └── provider/        # Provider implementations + router
web/                     # Admin dashboard (React + Vite + Capacitor)
├── src/
│   ├── api/             # API client for /admin/* endpoints
│   ├── components/      # Sidebar, Header, MobileTabBar
│   ├── pages/           # Dashboard, Models, Providers, Logs, Settings
│   └── styles/          # Responsive CSS
├── capacitor.config.json # Android packaging config
└── vite.config.ts       # Dev proxy + build config
```

## Deployment

Runs as a systemd service on the production server (8.141.97.21:4001), deployed via GitHub Actions.
See [[server-deployment]] for details.

## Related

- [[provider-config]] — How to add/modify providers
- [[fallback-chains]] — Auto-fallback routing logic
- [[model-aliases]] — Haiku/Sonnet/Opus tier naming
- [[admin-dashboard]] — Admin dashboard (React + Capacitor)
- [[source-codebase-2026-06-21]] — Latest state capture
