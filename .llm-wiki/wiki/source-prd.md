---
type: source
source_path: PRD.md
date: 2026-05-15
tags:
  - prd
  - requirements
  - architecture
---

# Source: PRD.md

## Summary

Product requirements document for the Claude Code Multi-Model Gateway. Defines the problem (multiple AI providers, single client limit), the solution (unified gateway), and the provider landscape.

## Key Takeaways

1. **Pain point**: Claude Code can only use one provider at a time, wasting unused model quotas.
2. **Solution**: A gateway that multiplexes requests to multiple providers with automatic fallback.
3. **GLM is primary**: Zhipu GLM is the primary provider, with MiMo and LongCat as fallbacks.
4. **Multi-protocol**: Supports OpenAI Chat Completions, Anthropic Messages, and OpenAI Responses API.
5. **Deployment**: Supports both local (`go build`) and server (Docker/GitHub Actions) deployment.

## Entities Mentioned

- [[glm-provider|智谱 GLM]] — Primary provider, coding plan API
- [[mimo-provider|小米 MiMo]] — Fallback 1, Anthropic API
- [[longcat-provider|美团 LongCat]] — Fallback 2, OpenAI API
- [[easyclaw-provider|EasyClaw]] — Claude proxy via OpenAI format
- [[apifree-provider|APIFree]] — SkyClaw Agent models
- [[openrouter-provider|OpenRouter]] — Free fallback models + GPT-5.5
- [[chatgpt-codex-provider|ChatGPT Codex]] — OAuth-based, requires proxy
- [[copilot-provider|GitHub Copilot]] — Free educational tier
- [[deepv-provider|DeepV Server]] — Internal aggregation service

## Concepts Referenced

- [[model-aliases]] — Haiku/Sonnet/Opus tier mapping
- [[fallback-chains]] — Auto-fallback logic across providers
- [[provider-config]] — YAML-driven provider configuration
- [[auth-flow]] — Master key authentication

## Acceptance Criteria

All criteria are checked as completed:
- [x] Gateway completes a full coding task through Claude Code
- [x] `coding` fallback works (GLM → MiMo → LongCat)
- [x] Manual model switching via `/model` commands
- [x] `.env` is gitignored
- [x] Unauthenticated requests are rejected
