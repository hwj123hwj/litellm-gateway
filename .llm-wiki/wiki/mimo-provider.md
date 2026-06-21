---
type: entity
date: 2026-06-21
tags:
  - provider
  - mimo
  - xiaomi
  - fallback
  - updated
---

# MiMo Provider (小米) — Updated 2026-06-21

## Summary

Xiaomi MiMo AI model provider. Positioned as the **first fallback** after GLM.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://token-plan-cn.xiaomimimo.com/v1/chat/completions`
- **Auth**: `MIMO_API_KEY` environment variable
- **Format**: OpenAI Chat Completions

> **Note**: MiMo's type was changed from `anthropic` to `openai` in `providers.yaml`. The Anthropic endpoint still exists (`/anthropic/v1/messages`) and is used via `main.go`'s `setupDefaultProviders` for the `mimo-anthropic` provider instance.

## Models

| Alias | Model ID | Tier |
|-------|----------|------|
| `mimo-sonnet` | **mimo-v2.5** | Balanced / reasoning |
| `mimo-opus` | **mimo-v2.5-pro** | Flagship |

Per the [[model-aliases]] alias-only rule, only `mimo-sonnet` and `mimo-opus` appear in `/v1/models`. The raw IDs (`mimo-v2.5`, `mimo-v2.5-pro`) are hidden.

## Role in Fallback

MiMo is the second provider in the `coding` chain and first in `coding-anthropic`:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview
coding-anthropic: mimo-v2.5 → LongCat-2.0-Preview
```

## Changelog

- **2026-06-21**: Type changed from `anthropic` to `openai` in providers.yaml
- **2026-06-14**: Initial wiki entry

## See Also

- [[glm-provider]] — Primary provider
- [[longcat-provider]] — Secondary fallback
- [[fallback-chains]] — Fallback logic
- [[model-aliases]] — Alias-only exposure
