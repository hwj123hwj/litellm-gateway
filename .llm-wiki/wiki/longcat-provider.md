---
type: entity
date: 2026-06-14
tags:
  - provider
  - longcat
  - meituan
  - fallback
---

# LongCat Provider (美团)

## Summary

Meituan LongCat AI model provider. Positioned as the **second fallback** after GLM and MiMo.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://api.longcat.chat/openai/chat/completions`
- **Auth**: `LONGCAT_API_KEY` environment variable
- **Format**: OpenAI Chat Completions

## Models

| Alias | Model ID | Tier |
|-------|----------|------|
| `longcat-sonnet` | **LongCat-Flash-Chat** | Balanced |
| `longcat-opus` | **LongCat-2.0-Preview** | Flagship |

## Role in Fallback

LongCat is the third/last provider in the `coding` chain:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-Flash-Chat
coding-anthropic: mimo-v2.5 → LongCat-Flash-Chat
```

## Notes

- Known for long context support
- Uses OpenAI format, converted to/from Anthropic by the gateway when needed

## See Also

- [[glm-provider]] — Primary provider
- [[mimo-provider]] — First fallback
- [[fallback-chains]] — Fallback logic
