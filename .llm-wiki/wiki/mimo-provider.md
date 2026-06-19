---
type: entity
date: 2026-06-14
tags:
  - provider
  - mimo
  - xiaomi
  - fallback
---

# MiMo Provider (小米)

## Summary

Xiaomi MiMo AI model provider. Positioned as the **first fallback** after GLM, accessed via Anthropic-compatible API.

## API Details

- **Type**: `anthropic`
- **Endpoint**: `https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages`
- **Auth**: `MIMO_API_KEY` environment variable
- **Format**: Native Anthropic Messages API (no format conversion needed)

## Models

| Alias | Model ID | Tier |
|-------|----------|------|
| `mimo-sonnet` | **mimo-v2.5** | Balanced / reasoning |
| `mimo-opus` | **mimo-v2.5-pro** | Flagship |

## Role in Fallback

MiMo is the second provider in the `coding` chain and first in `coding-anthropic`:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-Flash-Chat
coding-anthropic: mimo-v2.5 → LongCat-Flash-Chat
```

## Notes

- Uses native Anthropic protocol — no OpenAI↔Anthropic conversion needed in the gateway
- MiMo's Anthropic endpoint means requests via `/v1/messages` are direct-passthrough

## See Also

- [[glm-provider]] — Primary provider
- [[longcat-provider]] — Secondary fallback
- [[fallback-chains]] — Fallback logic
