---
type: entity
date: 2026-06-21
tags:
  - provider
  - removed
  - openrouter
  - archive
---

# OpenRouter Provider — REMOVED

> ⚠️ **This provider was removed on 2026-06-21.** Kept for historical reference only.

## Summary

OpenRouter was previously used as a free model fallback source and gateway to GPT-5.5. It was removed because the service is no longer used.

## What Was Removed

- `internal/provider/openrouter.go` (entire file: `OpenRouterProvider`, `FetchFreeModels`, `ModelAlias`, cache logic)
- `OPENROUTER_API_KEY` config field and env var
- `setupOpenRouterProviders()` function in `main.go`
- `free` chain (which depended on OpenRouter models as fallbacks)
- All `free-*` dynamically-generated model aliases

## Historical Details

- **Type**: `openai`
- **Endpoint**: `https://openrouter.ai/api/v1/chat/completions`
- **Auth**: `OPENROUTER_API_KEY` environment variable
- Fetched free models at startup, cached for 6 hours
- Models were registered with `free-{alias}` naming pattern

## Removal Context

See [[source-codebase-2026-06-21]] for full changelog.

## Related

- [[source-codebase-2026-06-21]] — Removal details
- [[fallback-chains]] — `free` chain also removed
