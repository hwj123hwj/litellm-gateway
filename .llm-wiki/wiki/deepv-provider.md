---
type: entity
date: 2026-08-24
tags:
  - provider
  - deepv
  - active
---

# DeepV Provider — Active

## Summary

DeepV Server is the aggregation backend behind EasyCode / DeepVCode. It is implemented as a custom provider in `go-gateway` and provides two DeepSeek models (0.6x credits). The provider was removed in `6e87f34` (2026-07-31) and restored on 2026-08-24 with a trimmed model set.

## API Details

- **Type**: `custom` (DeepV-specific protocol format, based on GenAI)
- **Endpoint**: `https://api-code.deepvlab.ai/v1/chat/messages`
- **Streaming endpoint**: `.../messages` → `.../stream` (auto-derived)
- **Auth**: Automatic JWT token loading, no API key needed.

### Token Resolution Strategy
The gateway resolves the token using the following fallback sequence:
1. `~/.easycode-user/jwt-token.json` (modern Easy Code path)
2. `~/.deepv/jwt-token.json` (legacy DeepVCode path)

The gateway parses the `ExpiresAt` field, gracefully handling both second and millisecond timestamp formats, and caches the token until expiry.

### Request Headers
- `Authorization: Bearer <token>`
- `X-Git-Remotes` / `X-Git-Branch` — attached from the workdir (`DEEPV_WORK_DIR`, defaults to the gateway start dir) when inside a git repo.

## Supported Models (as of 2026-08-24)

| Gateway Model | Bound DeepV Model | Capabilities |
|---------------|-------------------|--------------|
| `deepseek-v4-flash` | `deepseek-v4-flash` | text, tool_calling, streaming, reasoning |
| `deepseek-v4-flash-vision-exp` | `deepseek-v4-flash-vision-exp` | text, vision, tool_calling, streaming, reasoning |

The vision model converts Anthropic `image`/OpenAI `image_url` blocks to GenAI `inlineData` (base64 data URIs) or `fileData` (remote URLs).

## Enable

Set in `go-gateway/.env`:
```
DEEPV_ENABLED=true
DEEPV_WORK_DIR=            # optional, defaults to gateway start dir
```

## Implementation Code

- Provider: `go-gateway/internal/provider/deepv.go`
- Registration: `go-gateway/main.go` → `setupDeepVProviders`
- Config flags: `go-gateway/internal/config/config.go` (`DeepVEnabled`, `DeepVWorkDir`)
- Tests: `go-gateway/internal/provider/deepv_test.go`

## Related

- [[overview]] — Architecture Overview
- [[provider-config]] — Config instructions
- [[fallback-chains]] — Routing structure
