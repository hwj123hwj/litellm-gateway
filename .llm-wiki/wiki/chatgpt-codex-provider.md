---
type: entity
date: 2026-06-14
tags:
  - provider
  - chatgpt
  - codex
  - proxy
  - oauth
---

# ChatGPT Codex Provider

## Summary

Special provider that accesses OpenAI's ChatGPT API directly using OAuth tokens from the Codex Desktop login session. Requires an HTTP proxy for access from mainland China.

## API Details

- **Type**: Responses API (Custom)
- **Auth**: Reads OAuth token from `~/.codex/auth.json` (automatically refreshed)
- **Proxy**: Requires `HTTP_PROXY` env var (typically `http://127.0.0.1:7890`)
- **Format**: OpenAI Responses API passthrough (no format conversion)

## Models

| Model ID | Notes |
|----------|-------|
| `gpt-5.5` | ChatGPT Plus/Pro |
| `gpt-5.5-pro` | ChatGPT Pro only |
| `gpt-5.4-mini` | Lightweight |
| `o4-mini` | Reasoning model |

## Configuration

Enabled only when `HTTP_PROXY` is set in the environment. If not set, the provider is silently skipped.

```env
HTTP_PROXY=http://127.0.0.1:7890
```

## Architecture

- Uses a custom `http.Transport` with `ProxyURL` set
- Token auto-refreshes from `~/.codex/auth.json`
- All requests are passthrough to ChatGPT API — no format conversion
- Requests go through `/v1/responses` endpoint

## Important

This is the **only** provider in the gateway that uses the proxy. All other providers (GLM, MiMo, LongCat, etc.) create a plain `http.Client` with no proxy configuration.

## See Also

- [[source-env-example]] — HTTP_PROXY configuration
- [[fallback-chains]] — Provider chains
- [[overview]] — Architecture overview
