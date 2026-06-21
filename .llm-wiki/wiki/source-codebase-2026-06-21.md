---
type: source
source_path: go-gateway/ (full codebase scan)
date: 2026-06-21
tags:
  - codebase
  - refactor
  - cleanup
  - openrouter-removed
  - alias-only
---

# Source: Codebase State (2026-06-21)

## Summary

Full codebase scan capturing a cleanup pass that removed OpenRouter, the `free` chain, and fixed model alias exposure to avoid duplicate entries in `/v1/models`.

## Key Changes

### 1. Alias-Only Model Exposure

**Changed in**: `internal/provider/config_loader.go` (`SetupProvidersFromConfig`)

When a model in `providers.yaml` has `aliases` defined, only the aliases are registered as chains. The raw model ID is **no longer** exposed in `/v1/models`. This prevents duplicate entries (e.g. both `mimo-v2.5-pro` and `mimo-opus` appearing).

Models without aliases (e.g. `glm-4.7-flash`) continue to be registered by their raw ID.

**Before:**
```
mimo-v2.5-pro  ← raw ID (exposed)
mimo-opus      ← alias (exposed)
```

**After:**
```
mimo-opus      ← alias only (raw ID hidden)
```

### 2. OpenRouter Removed Entirely

- **Deleted**: `internal/provider/openrouter.go` (214 lines — `OpenRouterProvider`, `FetchFreeModels`, `ModelAlias`, cache logic)
- **Removed from** `main.go`: `setupOpenRouterProviders()` function and its call
- **Removed from** `internal/config/config.go`: `OpenRouterAPIKey` field and `OPENROUTER_API_KEY` env read
- **Removed from** `providers.yaml`: `free` chain definition
- **Removed from** `internal/provider/router.go`: `"free"` entry in `mapModelName` mappings
- **Removed from** `.env.example`: `OPENROUTER_API_KEY`, `APIFREE_API_KEY`
- **Removed from** `README.md`: OpenRouter section, APIFree section, `OPENROUTER_API_KEY` from env table and deploy secrets

### 3. `free` Chain Removed

The `free` fallback chain (which used `glm-4.7-flash` as primary and OpenRouter models as fallbacks) has been completely removed:

- Removed from `providers.yaml` chains section
- Removed from `setupDefaultProviders()` in `main.go`
- Removed from `mapModelName` mappings in `router.go`

### 4. README Architecture Diagram Updated

Added `CopilotProvider` and `DeepVProvider` to the architecture ASCII diagram, which previously only listed OpenAI, Anthropic, and ChatGPT providers.

## Current Providers

| Provider | Type | Env Var | Models |
|----------|------|---------|--------|
| **glm** | openai | `GLM_API_KEY` | glm-5.2(→glm-opus), glm-5-turbo(→glm-sonnet), glm-4.7(→glm-haiku), glm-4.7-flash |
| **mimo** | openai | `MIMO_API_KEY` | mimo-v2.5(→mimo-sonnet), mimo-v2.5-pro(→mimo-opus) |
| **longcat** | openai | `LONGCAT_API_KEY` | LongCat-2.0-Preview(→longcat-opus) |
| **easyclaw** | openai | `EASYCLAW_API_KEY` | claude-sonnet-4-6(→easyclaw-sonnet), claude-opus-4-6(→easyclaw-opus) |
| **deepv** | custom | `DEEPV_ENABLED` | deepseek-v4-flash, deepseek-v4-pro, glm-5, claude-sonnet-4-6, kimi-k2.6 |
| **copilot** | custom | `COPILOT_TOKEN` | gemini-3.1-pro-preview(→copilot-opus), gemini-3-flash-preview(→copilot-sonnet), gpt-4o(→copilot-haiku) |
| **chatgpt** | responses | `HTTP_PROXY` | gpt-5.5, gpt-5.5-pro, gpt-5.4-mini, gpt-5.4, gpt-5, o4-mini |

## Current Chains

| Chain | Providers (ordered) |
|-------|---------------------|
| `coding` | glm-glm-5-turbo → mimo-mimo-v2.5 → longcat-LongCat-2.0-Preview |
| `coding-anthropic` | mimo-mimo-v2.5 → longcat-LongCat-2.0-Preview |

## Current Config Fields (config.go)

`Port`, `LogLevel`, `MasterKey`, `GLMAPIKey`, `MIMOAPIKey`, `LongcatAPIKey`, `EasyClawAPIKey`, `CopilotToken`, `CopilotGithubToken`, `DeepVEnabled`, `DeepVWorkDir`, `HTTPProxy`, `Env`

**Removed fields**: `OpenRouterAPIKey`, `APIFreeAPIKey` (if it existed)

## Environment Variables (.env.example)

Required: `LITELLM_MASTER_KEY`
Optional: `GLM_API_KEY`, `MIMO_API_KEY`, `LONGCAT_API_KEY`, `EASYCLAW_API_KEY`, `DEEPV_ENABLED`, `DEEPV_WORK_DIR`, `HTTP_PROXY`, `COPILOT_TOKEN`, `COPILOT_GITHUB_TOKEN`, `PORT`, `LOG_LEVEL`

**Removed**: `OPENROUTER_API_KEY`, `APIFREE_API_KEY`

## Contradictions with Existing Wiki

| Existing Wiki Claim | Reality (2026-06-21) |
|---------------------|---------------------|
| `openrouter-provider.md` documents OpenRouter as active | **OpenRouter removed entirely** — file should be deleted or archived |
| `overview.md` lists APIFree and OpenRouter as providers | **Both removed** |
| `fallback-chains.md` lists `free` chain | **`free` chain removed** |
| `source-providers-yaml.md` lists OpenRouter provider row | **Removed from providers.yaml** |
| `source-env-example.md` lists `OPENROUTER_API_KEY` | **Removed from .env.example** |
| `mimo-provider.md` says type is `anthropic` | **Now `openai`** in providers.yaml |
| `longcat-provider.md` lists `longcat-sonnet` / LongCat-Flash-Chat | **Removed** (already noted in 2026-06-19 changelog but entity page not updated) |
| `glm-provider.md` references `free` chain with glm-4.7-flash | **`free` chain removed** |
| `provider-config.md` references `openrouter.go` | **File deleted** |

## Related

- [[config-loader]] — Alias-only registration logic
- [[provider-config]] — Configuration guide
- [[model-aliases]] — Tier naming
