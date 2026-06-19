---
type: entity
date: 2026-06-14
tags:
  - provider
  - glm
  - zhipu
  - primary
---

# GLM Provider (智谱)

## Summary

Primary AI model provider via Zhipu BigModel's coding plan API. Positioned as the first choice in the gateway, with MiMo and LongCat as fallbacks.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://open.bigmodel.cn/api/coding/paas/v4/chat/completions`
- **Auth**: `GLM_API_KEY` environment variable
- **Format**: OpenAI Chat Completions (converted to/from Anthropic format by the gateway)

## Models (as of 2026-06-14)

| Alias | Model ID | Tier | Notes |
|-------|----------|------|-------|
| `glm-opus` | **glm-5.2** | Flagship | Latest flagship (added 2026-06) |
| `glm-sonnet` | **glm-5-turbo** | Balanced | Primary coding model |
| `glm-haiku` | **glm-4.7** | Lightweight | Fast/cheap option |
| *(no alias)* | **glm-4.7-flash** | Free | In `free` fallback chain |

## Role in Fallback

The `coding` chain defaults to glm-5-turbo as the first provider:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-Flash-Chat
```

The `free` chain includes glm-4.7-flash:

```
free: glm-4.7-flash → deepseek → kimi
```

## Configuration

In [[source-providers-yaml]]:

```yaml
- name: glm
  type: openai
  url: https://open.bigmodel.cn/api/coding/paas/v4/chat/completions
  api_key_env: GLM_API_KEY
  models:
    - id: glm-5.2        aliases: [glm-opus]
    - id: glm-5-turbo    aliases: [glm-sonnet]
    - id: glm-4.7        aliases: [glm-haiku]
    - id: glm-4.7-flash  # free, no alias
```

## History

- **2026-06-14**: glm-5.2 added as new `glm-opus` (replacing glm-5.1). Removed glm-5.1, glm-4.5-air, and glm-flash alias per simplification decision.
- Original: Had glm-4.7, glm-5-turbo, glm-5.1 mapped to haiku/sonnet/opus.

## See Also

- [[model-aliases]] — Tier naming convention
- [[fallback-chains]] — How GLM fits into fallback logic
- [[mimo-provider]] — Primary fallback
- [[longcat-provider]] — Secondary fallback
