---
type: concept
date: 2026-06-14
tags:
  - configuration
  - providers
  - yaml
  - howto
---

# Provider Configuration (providers.yaml)

## Summary

Most new providers or models can be added by editing `providers.yaml` alone — no code changes needed. Only specific providers (DeepV, ChatGPT Codex, Copilot) require code changes in `main.go`.

## Adding a New Provider

Add to the `providers` array in [[source-providers-yaml]]:

```yaml
- name: new-provider          # Unique provider identifier
  type: openai                # "openai" or "anthropic"
  url: https://api.example.com/v1/chat/completions
  api_key_env: NEW_API_KEY    # Env var name for API key
  models:
    - id: model-v1
      aliases: [alias1, alias2]  # Optional aliases
```

Then add to `.env`:
```env
NEW_API_KEY=sk-xxx
```

Restart the gateway.

## Adding a Model to Existing Provider

Add an entry under the existing provider's `models` list:

```yaml
  - name: glm
    models:
      - id: glm-new-model
        aliases: [new-alias]
```

## Custom Provider Name

Use `provider_name` to override the auto-generated name (default: `供应商名-模型ID`):

```yaml
models:
  - id: openai/gpt-5.5
    aliases: [gpt-5.5]
    provider_name: or-gpt55    # Custom name for chain references
```

## Providers That Need Code Changes

- **DeepV Server** — Hardcoded in `main.go` (`setupDeepVProviders`)
- **ChatGPT Codex** — Hardcoded in `main.go` (`setupChatGPTProvider`)
- **GitHub Copilot** — Hardcoded in `main.go` (`setupCopilotProvider`)

## Provider Types

| Type | Handler Class | Format |
|------|--------------|--------|
| `openai` | `OpenAIProvider` | OpenAI ↔ Anthropic conversion |
| `anthropic` | `AnthropicProvider` | Native Anthropic passthrough |

## Related

- [[source-providers-yaml]] — Current complete config
- [[source-env-example]] — Required env vars
- [[glm-provider]] — Example provider setup
