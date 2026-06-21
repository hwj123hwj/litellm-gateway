---
type: source
source_path: go-gateway/providers.yaml
date: 2026-06-21
tags:
  - configuration
  - providers
  - models
  - yaml
  - updated
---

# Source: providers.yaml (Updated 2026-06-21)

## Current Providers

| Provider | Type | URL | Models |
|----------|------|-----|--------|
| **glm** | openai | open.bigmodel.cn/coding/paas/v4 | glm-5.2(→glm-opus), glm-5-turbo(→glm-sonnet), glm-4.7(→glm-haiku), glm-4.7-flash |
| **mimo** | openai | token-plan-cn.xiaomimimo.com/v1 | mimo-v2.5(→mimo-sonnet), mimo-v2.5-pro(→mimo-opus) |
| **longcat** | openai | api.longcat.chat | LongCat-2.0-Preview(→longcat-opus) |
| **easyclaw** | openai | api.easyclaw.work | claude-sonnet-4-6(→easyclaw-sonnet), claude-opus-4-6(→easyclaw-opus) |

> **Note**: Models with aliases only expose the alias in `/v1/models` (see [[model-aliases]]). The `→` notation shows alias mapping.

## Active Chains

| Chain | Providers (in order) |
|-------|---------------------|
| `coding` | glm-glm-5-turbo → mimo-mimo-v2.5 → longcat-LongCat-2.0-Preview |
| `coding-anthropic` | mimo-mimo-v2.5 → longcat-LongCat-2.0-Preview |

## Changelog

### 2026-06-21
- **Removed**: `free` chain (previously: glm-4.7-flash → deepseek → kimi)
- **Removed**: OpenRouter provider entirely
- **Changed**: `mimo` provider type from `anthropic` to `openai`
- **Changed**: Alias-only model exposure — raw model IDs no longer registered as chains when aliases exist

### 2026-06-19
- **Added**: glm-5.2 as new `glm-opus` flagship
- **Removed**: glm-5.1 (`glm-opus-pro`), glm-4.5-air (`glm-air`)
- **Removed**: LongCat-Flash-Chat (`longcat-sonnet`)
- **Removed**: Entire APIFree/SkyClaw provider
- **Changed**: `coding` chain uses LongCat-2.0-Preview instead of Flash-Chat
- **Changed**: `free` chain added glm-4.7-flash as first provider

## Providers That Require Code (main.go)

These are not in `providers.yaml` — they are hardcoded in `main.go`:

| Provider | Setup Function | Notes |
|----------|---------------|-------|
| DeepV Server | `setupDeepVProviders()` | Gated by `DEEPV_ENABLED` |
| ChatGPT Codex | `setupChatGPTProvider()` | Requires `HTTP_PROXY` |
| GitHub Copilot | `setupCopilotProviders()` | Requires `COPILOT_TOKEN` |

## Related

- [[provider-config]] — How to add/modify providers
- [[fallback-chains]] — Chain routing logic
- [[model-aliases]] — Tier naming system
- [[glm-provider]], [[mimo-provider]], [[longcat-provider]]
