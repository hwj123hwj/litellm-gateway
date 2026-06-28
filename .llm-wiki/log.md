# LLM Wiki Log

> Chronological record of wiki operations.

## [2026-06-29] synthesis | Mobile app (React Native) performance pass
- **New synthesis page**: mobile-app-performance (full audit + fixes)
- **Updated**: index.md (registered synthesis page)
- **Key changes (code)**:
  - Replaced nested empty-data FlashList in DashboardScreen with ScrollView
  - Moved sort logic into useMemo (non-mutating) across Dashboard/Models screens
  - Switched all screens + App.tsx to atomic Zustand selectors
  - Removed duplicate 15s health polling + derived-state useState in App.tsx
  - Stable keyExtractor in LogsScreen (timestamp|model|path instead of index)
  - Extracted formatNumber/formatLatency/formatRelativeTime/abbreviate to src/utils
  - Extracted memoized list-item components to src/components/dashboard
  - Added AbortSignal plumbing to api/client.ts fetchJSON and get* functions
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-26] ingest | DeepV token path compatibility update
- **New entity pages**: deepv-provider (DeepV Server custom provider specs)
- **Updated**: index.md (registered deepv-provider entity)
- **Key changes**:
  - Added support for modern `~/.easycode-user/jwt-token.json` location fallback in `deepv.go`.
  - Added millisecond timestamp handling for DeepV auth tokens.

## [2026-06-21] ingest | Codebase state (OpenRouter removal + alias-only exposure)
- **New source pages**: source-codebase-2026-06-21 (full codebase state capture)
- **Updated concept pages**: overview, fallback-chains, model-aliases, provider-config
- **Updated entity pages**: glm-provider, mimo-provider, longcat-provider, openrouter-provider (→ REMOVED archive)
- **Updated source pages**: source-providers-yaml, source-env-example
- **Updated**: index.md (new source page, annotated updates)
- **Key changes**:
  - OpenRouter provider removed entirely (openrouter.go deleted, config field removed)
  - `free` chain removed from providers.yaml and main.go
  - Alias-only model exposure: models with aliases no longer expose raw model ID in /v1/models
  - APIFree_API_KEY removed from .env.example
  - mimo provider type corrected: anthropic → openai
  - longcat-provider page corrected: removed stale longcat-sonnet references

## [2026-06-19] ingest | Project root (CI/CD + model updates + server deployment)
- **New source pages**: source-deploy-yml (CI/CD pipeline)
- **New entity pages**: server-deployment (8.141.97.21:4001)
- **Updated concept pages**: fallback-chains, model-aliases (removed SkyClaw/longcat-sonnet/extra GLM models)
- **Updated entity pages**: source-providers-yaml (full changelog)
- **Index**: Added new pages, annotated updates on changed pages
- **Key changes**: glm-opus→5.2, port→4001, CI/CD rewrote to Go native+systemd, server migration

## [2026-06-14] ingest | Project root (PRD, README, providers.yaml, .env.example)
- **Source summaries created**: source-prd, source-go-gateway-readme, source-providers-yaml, source-env-example
- **Entity pages created**: glm-provider, mimo-provider, longcat-provider, openrouter-provider, chatgpt-codex-provider
- **Concept pages created**: provider-config, fallback-chains, model-aliases
- **Updated**: index.md with full page listing, overview.md (pre-existing)

## [2026-06-14] init | Wiki Initialized
- Created wiki directory structure
- Ready for source ingestion
