---
type: source
source_path: go-gateway/README.md
date: 2026-05-15
tags:
  - readme
  - documentation
  - deployment
  - api
---

# Source: go-gateway/README.md

## Summary

Comprehensive operational documentation for the LLM Gateway. Covers quick start, API endpoints, available models, environment variables, architecture, development, and deployment.

## Key Facts

- **Memory**: ~18 MB (vs LiteLLM's ~570 MB)
- **Startup**: <1 second
- **Binary size**: ~15 MB
- **Port**: 4001 (configurable via `PORT`)
- **Go version**: 1.21+

## API Endpoints

| Endpoint | Method | Auth | Purpose |
|----------|--------|------|---------|
| `/health` | GET | None | Health check |
| `/v1/models` | GET | Bearer | List models |
| `/v1/chat/completions` | POST | Bearer | OpenAI compatible |
| `/v1/messages` | POST | Bearer | Anthropic compatible |
| `/v1/responses` | POST | Bearer | OpenAI Responses (Codex CLI) |
| `/chat/completions`, `/messages`, `/responses` | POST | Bearer | Short-path aliases |

## Environment Variables (14 total)

| Variable | Required | Purpose |
|----------|----------|---------|
| `LITELLM_MASTER_KEY` | Yes | Gateway auth token |
| `GLM_API_KEY` | No | Zhipu API key |
| `MIMO_API_KEY` | No | Xiaomi API key |
| `LONGCAT_API_KEY` | No | Meituan API key |
| `EASYCLAW_API_KEY` | No | EasyClaw key |
| `APIFREE_API_KEY` | No | APIFree key |
| `OPENROUTER_API_KEY` | No | OpenRouter key |
| `DEEPV_ENABLED` | No | Enable DeepV Server |
| `DEEPV_WORK_DIR` | No | DeepV work dir |
| `COPILOT_TOKEN` | No | GitHub Copilot token |
| `COPILOT_GITHUB_TOKEN` | No | Copilot refresh token |
| `HTTP_PROXY` | No | Proxy for ChatGPT Codex |
| `PORT` | No | Listen port (default: 4001) |
| `LOG_LEVEL` | No | Log level (default: info) |

## Deployment Methods

1. **Local**: `go build -o gateway . && ./gateway`
2. **GitHub Actions**: Auto-deploy on push to `main`
3. **Docker**: `docker run -p 4001:4001 --env-file .env go-llm-gateway`
4. **macOS launchd**: `launchctl load ~/Library/LaunchAgents/local.go-gateway.plist`

## Adding a New Provider

1. Create file in `internal/provider/` (e.g., `baidu.go`)
2. Implement `Provider` interface
3. Add config field in `config.go`
4. Register in `main.go`
5. Add model mapping in `router.go`

> Note: For most providers, just editing `providers.yaml` is sufficient — no code needed.

## Related

- [[provider-config]] — No-code provider addition
- [[overview]] — Architecture and design
