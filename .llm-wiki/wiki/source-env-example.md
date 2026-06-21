---
type: source
source_path: go-gateway/.env.example
date: 2026-06-21
tags:
  - configuration
  - environment
  - env-vars
  - updated
---

# Source: .env.example (Updated 2026-06-21)

## Current Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LITELLM_MASTER_KEY` | Yes | — | Gateway auth token |
| `GLM_API_KEY` | No | — | Zhipu GLM API key |
| `MIMO_API_KEY` | No | — | Xiaomi MiMo API key |
| `LONGCAT_API_KEY` | No | — | Meituan LongCat API key |
| `EASYCLAW_API_KEY` | No | — | EasyClaw (Claude proxy) API key |
| `DEEPV_ENABLED` | No | false | Enable DeepV Server |
| `DEEPV_WORK_DIR` | No | — | DeepV working directory |
| `HTTP_PROXY` | No | — | HTTP proxy for ChatGPT Codex |
| `COPILOT_TOKEN` | No | — | GitHub Copilot token (~30 min TTL) |
| `COPILOT_GITHUB_TOKEN` | No | — | GitHub OAuth for auto-refreshing Copilot |
| `PORT` | No | 4001 | Listen port |
| `LOG_LEVEL` | No | info | Log level |

## Removed Variables

| Variable | Removed | Reason |
|----------|---------|--------|
| `OPENROUTER_API_KEY` | 2026-06-21 | OpenRouter provider removed |
| `APIFREE_API_KEY` | 2026-06-21 | APIFree provider was already removed (2026-06-19) |

## Default .env.example Content

```env
LITELLM_MASTER_KEY=sk-local-gateway-xxx
GLM_API_KEY=
MIMO_API_KEY=
LONGCAT_API_KEY=
EASYCLAW_API_KEY=
DEEPV_ENABLED=false
DEEPV_WORK_DIR=
HTTP_PROXY=http://127.0.0.1:7890
# COPILOT_TOKEN=
# COPILOT_GITHUB_TOKEN=
PORT=4001
LOG_LEVEL=info
```

## Related

- [[provider-config]] — Provider setup guide
- [[source-codebase-2026-06-21]] — Removal details
