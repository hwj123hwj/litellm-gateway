---
type: concept
date: 2026-06-21
tags:
  - naming
  - models
  - convention
  - updated
---

# Model Aliases & Tier Naming (Updated 2026-06-21)

## Convention

Core providers (GLM, MiMo, LongCat) use standardized tier naming:

| Tier | Meaning | Characteristics |
|------|---------|-----------------|
| **opus** | Flagship | Highest capability, most expensive |
| **sonnet** | Balanced | Best cost/performance ratio |
| **haiku** | Lightweight | Fastest, cheapest |

Other providers use `{provider}-{tier}` format (e.g. `easyclaw-sonnet`, `copilot-opus`).

## Current Mappings (providers.yaml)

| Alias | Provider | Actual Model |
|-------|----------|-------------|
| `glm-opus` | GLM | glm-5.2 |
| `glm-sonnet` | GLM | glm-5-turbo |
| `glm-haiku` | GLM | glm-4.7 |
| `mimo-sonnet` | MiMo | mimo-v2.5 |
| `mimo-opus` | MiMo | mimo-v2.5-pro |
| `longcat-opus` | LongCat | LongCat-2.0-Preview |
| `easyclaw-sonnet` | EasyClaw | claude-sonnet-4-6 |
| `easyclaw-opus` | EasyClaw | claude-opus-4-6 |

## Exposure Rule (Changed 2026-06-21)

Models with aliases only expose the alias in `/v1/models`. The raw model ID is hidden to avoid duplicates. Models without aliases (e.g. `glm-4.7-flash`) expose their raw ID.

This is implemented in `config_loader.go` (`SetupProvidersFromConfig`):
- `len(mc.Aliases) > 0` → register aliases only
- `len(mc.Aliases) == 0` → register raw model ID

## Copilot Mappings (router.go mapModelName, not in providers.yaml)

| Alias | Actual Model |
|-------|-------------|
| `copilot-opus` | gemini-3.1-pro-preview |
| `copilot-sonnet` | gemini-3-flash-preview |
| `copilot-haiku` | gpt-4o-2024-11-20 |

## Changelog

### 2026-06-21
- **Changed**: Alias-only exposure — models with aliases no longer expose raw model ID in `/v1/models`
- **Removed**: All OpenRouter `free-*` aliases (OpenRouter provider removed)
- **Removed**: `free` chain (no longer available as a model name)

### 2026-06-19
- **Removed**: `glm-opus-pro` (glm-5.1), `glm-air` (glm-4.5-air), `glm-flash` (glm-4.7-flash alias)
- **Removed**: `longcat-sonnet` — LongCat now only has opus tier
- **Removed**: `sky-opus`, `sky-lite` — APIFree provider removed entirely
- **Changed**: `glm-opus` now maps to glm-5.2 (was glm-5.1)

## Related

- [[glm-provider]] — Current GLM tier mappings
- [[fallback-chains]] — How tiers feed into chains
- [[provider-config]] — How to register models
- [[source-codebase-2026-06-21]] — Alias-only exposure rule
