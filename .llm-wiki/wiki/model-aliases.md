---
type: concept
date: 2026-06-19
tags:
  - naming
  - models
  - convention
  - updated
---

# Model Aliases & Tier Naming (Updated 2026-06-19)

## Convention

Core providers (GLM, MiMo, LongCat) use standardized tier naming:

| Tier | Meaning | Characteristics |
|------|---------|-----------------|
| **opus** | Flagship | Highest capability, most expensive |
| **sonnet** | Balanced | Best cost/performance ratio |
| **haiku** | Lightweight | Fastest, cheapest |

## Current Mappings

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

## Changelog (2026-06-19)

- **Removed**: `glm-opus-pro` (glm-5.1), `glm-air` (glm-4.5-air), `glm-flash` (glm-4.7-flash alias)
- **Removed**: `longcat-sonnet` — LongCat now only has opus tier
- **Removed**: `sky-opus`, `sky-lite` — APIFree provider removed entirely
- **Changed**: `glm-opus` now maps to glm-5.2 (was glm-5.1)

## Related

- [[glm-provider]] — Current GLM tier mappings
- [[fallback-chains]] — How tiers feed into chains
- [[provider-config]] — How to register models
