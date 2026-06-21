---
type: concept
date: 2026-06-21
tags:
  - routing
  - fallback
  - chains
  - reliability
  - updated
---

# Fallback Chains (Updated 2026-06-21)

## Summary

When the primary provider fails (network error, rate limiting, API error), the gateway automatically tries the next provider in the chain.

## Mechanism

1. Client sends request with `"model": "coding"` (a chain name)
2. Router looks up chain `["glm-glm-5-turbo", "mimo-mimo-v2.5", "longcat-LongCat-2.0-Preview"]`
3. Tries first provider — if success → return response
4. If error → log failure, try next provider
5. If all providers fail → return error to client

## Active Chains (providers.yaml)

| Chain Name | Providers (ordered) | Use Case |
|------------|---------------------|----------|
| `coding` | glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview | General coding (GLM primary) |
| `coding-anthropic` | mimo-v2.5 → LongCat-2.0-Preview | Anthropic protocol |

## Chains Registered in Code (main.go)

These chains are registered when `providers.yaml` is not found (default fallback path):

| Chain | Providers |
|-------|-----------|
| `coding` | glm → mimo → longcat |
| `coding-anthropic` | glm-anthropic → mimo-anthropic → longcat-anthropic |
| `glm-opus` / `glm-sonnet` / `glm-haiku` | glm |
| `glm-flash` | glm-free |
| `mimo-sonnet` / `mimo-opus` | mimo |
| `longcat-opus` | longcat |
| `easyclaw-sonnet` / `easyclaw-opus` | easyclaw |
| `copilot-opus` / `copilot-sonnet` / `copilot-haiku` | copilot |
| `gpt-5.5` / `gpt-5.5-pro` / `gpt-5.4-mini` / `gpt-5.4` / `gpt-5` / `o4-mini` | chatgpt |
| `deepv-deepseek-flash` / `deepv-deepseek-pro` / `deepv-glm5` / `deepv-claude-sonnet` / `deepv-kimi` | deepv-* |

## Changelog

### 2026-06-21
- **Removed**: `free` chain entirely (previously: glm-4.7-flash → OpenRouter deepseek → OpenRouter kimi)
- `coding` and `coding-anthropic` are now the only chains defined in `providers.yaml`

### 2026-06-19
- `coding` chain: LongCat-Flash-Chat replaced with LongCat-2.0-Preview
- `free` chain: Added glm-4.7-flash as first provider (was deepseek only)

## Related

- [[provider-config]] — Provider/model setup
- [[model-aliases]] — Tier naming
- [[source-providers-yaml]] — Current chain definitions in YAML
- [[source-codebase-2026-06-21]] — Latest codebase state
- [[glm-provider]], [[mimo-provider]], [[longcat-provider]]
