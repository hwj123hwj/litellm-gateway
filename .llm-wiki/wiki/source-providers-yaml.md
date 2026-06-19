---
type: source
source_path: go-gateway/providers.yaml
date: 2026-06-19
tags:
  - configuration
  - providers
  - models
  - yaml
  - updated
---

# Source: providers.yaml (Updated 2026-06-19)

## Changelog from previous version

- **➕ Added**: glm-5.2 as new `glm-opus` flagship
- **➖ Removed**: glm-5.1 (`glm-opus-pro`), glm-4.5-air (`glm-air`)
- **➖ Removed**: LongCat-Flash-Chat (`longcat-sonnet`)
- **➖ Removed**: Entire APIFree/SkyClaw provider (balance exhausted)
- **🔄 Changed**: `coding` chain now uses LongCat-2.0-Preview instead of Flash-Chat
- **🔄 Changed**: `free` chain now starts with glm-4.7-flash

## Current Providers (as of 2026-06-19)

| Provider | Type | URL | Models |
|----------|------|-----|--------|
| **glm** | openai | open.bigmodel.cn/coding/paas/v4 | glm-5.2(opus), glm-5-turbo(sonnet), glm-4.7(haiku), glm-4.7-flash |
| **mimo** | anthropic | token-plan-cn.xiaomimimo.com | mimo-v2.5(sonnet), mimo-v2.5-pro(opus) |
| **longcat** | openai | api.longcat.chat | LongCat-2.0-Preview(opus) |
| **easyclaw** | openai | api.easyclaw.work | claude-sonnet-4-6, claude-opus-4-6 |
| **openrouter** | openai | openrouter.ai | gpt-5.5, deepseek free, kimi free |

## Active Chains

| Chain | Providers (in order) |
|-------|---------------------|
| `coding` | glm-5-turbo → mimo-v2.5 → LongCat-2.0-Preview |
| `coding-anthropic` | mimo-v2.5 → LongCat-2.0-Preview |
| `free` | glm-4.7-flash → deepseek → kimi |

## Removed Providers

- **APIFree (SkyClaw)**: sky-opus and sky-lite removed due to `insufficient_balance` on API key
- **LongCat sonnet tier**: LongCat-Flash-Chat removed, only opus tier remains

## Related

- [[provider-config]] — How to add/modify providers
- [[fallback-chains]] — Chain routing logic
- [[model-aliases]] — Tier naming system
- [[glm-provider]], [[mimo-provider]], [[longcat-provider]]
