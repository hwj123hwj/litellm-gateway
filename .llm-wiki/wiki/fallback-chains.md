---
type: concept
date: 2026-06-19
tags:
  - routing
  - fallback
  - chains
  - reliability
  - updated
---

# Fallback Chains (Updated 2026-06-19)

## Summary

When the primary provider fails (network error, rate limiting, API error), the gateway automatically tries the next provider in the chain.

## Mechanism

1. Client sends request with `"model": "coding"` (a chain name)
2. Router looks up chain `["glm-glm-5-turbo", "mimo-mimo-v2.5", "longcat-LongCat-2.0-Preview"]`
3. Tries first provider — if success → return response
4. If error → log failure, try next provider
5. If all providers fail → return error to client

## Active Chains

| Chain Name | Providers (ordered) | Use Case |
|------------|---------------------|----------|
| `coding` | glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview | General coding (GLM primary) |
| `coding-anthropic` | mimo-v2.5 → LongCat-2.0-Preview | Anthropic protocol |
| `free` | glm-4.7-flash → deepseek → kimi | Zero-cost fallback |

## Changelog (2026-06-19)

- `coding` chain: LongCat-Flash-Chat replaced with LongCat-2.0-Preview (sonnet tier removed from LongCat)
- `free` chain: Added glm-4.7-flash as first provider (was deepseek only)

## Related

- [[provider-config]] — Provider/model setup
- [[model-aliases]] — Tier naming
- [[source-providers-yaml]] — Current chain definitions
- [[glm-provider]], [[mimo-provider]], [[longcat-provider]]
