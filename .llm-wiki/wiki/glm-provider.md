---
type: entity
date: 2026-06-21
tags:
  - provider
  - glm
  - zhipu
  - primary
  - updated
---

# GLM Provider (智谱) — Updated 2026-06-21

## Summary

Primary AI model provider via Zhipu BigModel's coding plan API. Positioned as the first choice in the gateway, with MiMo and LongCat as fallbacks.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://open.bigmodel.cn/api/coding/paas/v4/chat/completions`
- **Auth**: `GLM_API_KEY` environment variable
- **Format**: OpenAI Chat Completions (converted to/from Anthropic format by the gateway)

## Models

| Alias | Model ID | Tier | Notes |
|-------|----------|------|-------|
| `glm-opus` | **glm-5.2** | Flagship | Latest flagship |
| `glm-sonnet` | **glm-5-turbo** | Balanced | Primary coding model |
| `glm-haiku` | **glm-4.7** | Lightweight | Fast/cheap option |
| *(no alias)* | **glm-4.7-flash** | Free | No alias → raw ID exposed in `/v1/models` |

Per the [[model-aliases]] alias-only rule, `glm-opus`, `glm-sonnet`, `glm-haiku` are exposed (aliases only, raw IDs hidden). `glm-4.7-flash` has no alias so its raw ID is exposed.

## Role in Fallback

The `coding` chain defaults to glm-5-turbo as the first provider:

```
coding: glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview
```

## Configuration

In [[source-providers-yaml]]:

```yaml
- name: glm
  type: openai
  url: https://open.bigmodel.cn/api/coding/paas/v4/chat/completions
  api_key_env: GLM_API_KEY
  models:
    - id: glm-5.2
      aliases: [glm-opus]
    - id: glm-5-turbo
      aliases: [glm-sonnet]
    - id: glm-4.7
      aliases: [glm-haiku]
    - id: glm-4.7-flash
```

## History

- **2026-06-21**: Updated for alias-only exposure; `free` chain references removed
- **2026-06-19**: glm-5.2 added as new `glm-opus` (replacing glm-5.1). Removed glm-5.1, glm-4.5-air, and glm-flash alias.

## See Also

- [[model-aliases]] — Tier naming convention
- [[fallback-chains]] — How GLM fits into fallback logic
- [[mimo-provider]] — Primary fallback
- [[longcat-provider]] — Secondary fallback
