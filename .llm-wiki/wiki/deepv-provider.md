---
type: entity
date: 2026-06-26
tags:
  - provider
  - deepv
  - active
---

# DeepV Provider — Active

## Summary

DeepV Server is an internal enterprise AI model aggregation service. It is directly implemented as a custom provider in `go-gateway`.

## API Details

- **Type**: `custom` (DeepV-specific protocol format, based on GenAI)
- **Endpoint**: `https://api-code.deepvlab.ai/v1/chat/messages`
- **Auth**: Automatic token loading. Reads from local JWT config files.

### Token Resolution Strategy
To support both legacy DeepVCode setups and modern Easy Code environments, the gateway resolves token using the following fallback sequence:
1. `~/.easycode-user/jwt-token.json` (modern Easy Code path)
2. `~/.deepv/jwt-token.json` (legacy DeepVCode path)

The gateway parses the `ExpiresAt` field, gracefully handling both second and millisecond timestamp formats to prevent cache expiration check bugs.

## Supported Models

| Gateway Chain/Alias | Bound DeepV Model | Notes |
|---------------------|-------------------|-------|
| `deepv-deepseek-flash` | `deepseek-v4-flash` | Lightweight fast DeepSeek |
| `deepv-deepseek-pro` | `deepseek-v4-pro` | Flagship deepseek pro model |
| `deepv-glm5` | `glm-5` | Zhipu GLM-5 |
| `deepv-claude-sonnet` | `claude-sonnet-4-6` | Claude Sonnet 4.6 |
| `deepv-kimi` | `kimi-k2.6` | Moonshot Kimi k2.6 |

## Implementation Code

- Main handler: `go-gateway/internal/provider/deepv.go`
- Initialization logic: `go-gateway/main.go` -> `setupDeepVProviders`

## Related

- [[overview]] — Architecture Overview
- [[provider-config]] — Config instructions
- [[fallback-chains]] — Routing structure
