---
type: entity
date: 2026-06-21
tags:
  - provider
  - longcat
  - meituan
  - fallback
  - updated
---

# LongCat Provider (美团) — Updated 2026-06-21

## Summary

Meituan LongCat AI model provider. Positioned as the **second fallback** after GLM and MiMo. Now only offers the opus tier.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://api.longcat.chat/openai/chat/completions`
- **Auth**: `LONGCAT_API_KEY` environment variable
- **Format**: OpenAI Chat Completions

## Models

| Alias | Model ID | Tier |
|-------|----------|------|
| `longcat-opus` | **LongCat-2.0-Preview** | Flagship |

Per the [[model-aliases]] alias-only rule, only `longcat-opus` appears in `/v1/models`. The raw ID (`LongCat-2.0-Preview`) is hidden.

> **Removed**: `longcat-sonnet` (LongCat-Flash-Chat) was removed in 2026-06-19. LongCat now only has the opus tier.

## Role in Fallback

LongCat is the third/last provider in the `coding` chain:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview
coding-anthropic: mimo-v2.5 → LongCat-2.0-Preview
```

## Changelog

- **2026-06-21**: Updated to reflect alias-only exposure, removed stale longcat-sonnet references
- **2026-06-19**: LongCat-Flash-Chat removed, LongCat-2.0-Preview is now the only model

## See Also

- [[glm-provider]] — Primary provider
- [[mimo-provider]] — First fallback
- [[fallback-chains]] — Fallback logic
