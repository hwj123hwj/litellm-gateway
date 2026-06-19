---
type: source
source_path: go-gateway/.env.example
date: 2026-05-15
tags:
  - configuration
  - environment
  - secrets
---

# Source: .env.example

## Summary

Template for the `.env` file containing all environment variable configuration. Not tracked in git — copy to `.env` and fill in values.

## All Variables

```
LITELLM_MASTER_KEY    # Required: gateway auth token
GLM_API_KEY            # Zhipu BigModel
MIMO_API_KEY           # Xiaomi MiMo
LONGCAT_API_KEY        # Meituan LongCat
EASYCLAW_API_KEY       # EasyClaw (Claude proxy)
OPENROUTER_API_KEY     # OpenRouter (free models)
DEEPV_ENABLED          # false - DeepV server toggle
DEEPV_WORK_DIR         # DeepV work directory
APIFREE_API_KEY        # SkyClaw agent models
HTTP_PROXY             # http://127.0.0.1:7890 - for ChatGPT Codex
COPILOT_TOKEN          # GitHub Copilot (30min expiry)
COPILOT_GITHUB_TOKEN   # Copilot refresh token
PORT                   # 4001
LOG_LEVEL              # info
```

## Important Notes

- `LITELLM_MASTER_KEY` is the only **required** variable
- Unconfigured providers are **silently skipped** — no crash
- `HTTP_PROXY` enables ChatGPT Codex provider when set (reads token from `~/.codex/auth.json`)
- `COPILOT_TOKEN` expires in ~30 minutes; use `COPILOT_GITHUB_TOKEN` for auto-refresh

## Related

- [[source-go-gateway-readme]] — Full env var documentation
- [[chatgpt-codex-provider]] — ChatGPT OAuth provider details
- [[auth-flow]] — Authentication setup
