---
type: entity
date: 2026-06-14
tags:
  - provider
  - openrouter
  - gpt
  - free
---

# OpenRouter Provider

## Summary

OpenRouter is used as the gateway to OpenAI's GPT-5.5 model and as a free model fallback source. It provides access to third-party hosted models via a unified API.

## API Details

- **Type**: `openai`
- **Endpoint**: `https://openrouter.ai/api/v1/chat/completions`
- **Auth**: `OPENROUTER_API_KEY` environment variable
- **Format**: OpenAI Chat Completions

## Models

| Alias | Model ID | Notes |
|-------|----------|-------|
| `gpt-5.5` | **openai/gpt-5.5** | Full GPT-5.5 via OpenRouter |
| `free-deepseek` | **deepseek/deepseek-v4-flash:free** | Free deepseek model |
| `free-kimi` | **moonshotai/kimi-k2.6:free** | Free kimi model |

## Role in Fallback

The `free` chain uses OpenRouter models as secondary fallbacks:

```
free: glm-4.7-flash → deepseek (OpenRouter) → kimi (OpenRouter)
```

## Important Note

The OpenRouter URL (`openrouter.ai`) is accessed **without proxy** — the `OpenAIProvider` in the gateway creates a plain `http.Client` with no proxy configuration. This means GPT-5.5 via the gateway works only if `openrouter.ai` is directly accessible from the network.

## See Also

- [[source-providers-yaml]] — Full config
- [[fallback-chains]] — Free fallback chain
- [[glm-provider]] — Free model also in the chain
