# LLM Wiki Log

> Chronological record of wiki operations.

## [2026-06-29] synthesis | Mobile app (React Native) — Round 9 fourth audit
- **Updated synthesis page**: mobile-app-performance (sections 45–47)
- **Context**: final deep code review via code-reviewer sub-agent
- **Bugs found & fixed**:
  - **#45 (HIGH)** App.tsx SetupScreen initialized URL input with 'http://' — clicking start without editing saved a malformed URL with no host, causing all subsequent API requests to fail with NETWORK error; user got stuck since URL was non-empty (setup screen skipped). Changed to empty string.
  - **#46 (MEDIUM)** client.ts getBaseUrl unconditionally appended /admin — a user pasting http://server:4001/admin would get double-pathed to /admin/admin causing 404s. Added regex check to skip appending if /admin suffix already present.
  - **#47 (LOW)** getLogs default limit=50 in client.ts vs limit=100 in store — inconsistency was a latent trap if client was called directly. Aligned both to 100.
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) — Round 8 third bug audit
- **Updated synthesis page**: mobile-app-performance (sections 42–44)
- **Context**: code-analysis sub-agent deep review of all source files
- **Bugs found & fixed**:
  - **#42 (MEDIUM)** usePolling had no debounce — on slow networks, each tick could fire while the previous fetch was still pending, wasting requests. Added isRunning ref guard to skip overlapping ticks.
  - **#43 (HIGH)** RequestGuard.set was typed as Partial<Record<string,unknown>>, bypassing TypeScript safety — a typo in key strings would compile silently and silently break state updates. Refactored to generic prefix-based key construction with compile-time validation against AppState; fixed StoreSet type to match zustand set signature.
  - **#44 (MEDIUM, build-breaking)** @shopify/flash-list@2.0.2 type defs do not declare estimatedItemSize — adding it caused tsc errors. Removed from all 3 FlashList instances.
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) — Round 7 bug audit
- **Updated synthesis page**: mobile-app-performance (sections 37–41)
- **Context**: second pass review after R6, focusing on dead code, UI consistency, and error-path robustness
- **Bugs found & fixed**:
  - **#37 (MEDIUM)** ModelRankRow accepted maxRequests prop but never rendered it — DashboardScreen wasted a useMemo computing it and triggered extra re-renders. Removed prop + memo.
  - **#38 (MEDIUM)** ProviderCard rendered raw item.requests instead of formatNumber() — inconsistent with all other cards, large numbers overflowed. Now uses formatNumber.
  - **#39 (LOW)** fetchJSON res.json() parse failure (malformed 200 body) was uncaught, surfaced as misleading NETWORK error. Now throws ApiError SERVER with accurate message.
  - **#40 (MEDIUM)** RequestGuard AbortError check was dead code — fetchJSON already wraps all AbortErrors into ApiError TIMEOUT. Removed dead branch.
  - **#41 (MEDIUM)** DashboardScreen polled fetchHealth every 10s but health data was never used by any UI — pure waste. Removed from polling callback.
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) — regression & bug audit (Round 6)
- **Updated synthesis page**: mobile-app-performance (sections 33–36)
- **Context**: reviewed commits 571e3bd..9a770e0 (R1–R5) for regressions/bugs introduced by the optimization refactors
- **Bugs found & fixed**:
  - **#33 (HIGH)** store RequestGuard created AbortController per call but never aborted the previous in-flight request — generation counter only prevented stale state writes, not wasted network calls. Now holds currentController ref and aborts it on each new run().
  - **#34 (HIGH)** DashboardScreen subscribed to `health` state via useStore but the value was unused (dead `status` var was removed in R2 but subscription remained) — every 10s health poll triggered a pointless DashboardScreen re-render. Removed the selector.
  - **#35 (MEDIUM)** ProvidersScreen FlashList with numColumns=2 kept ItemSeparatorComponent, which FlashList inserts between horizontal neighbors too, causing uneven card gutters. Removed separator; spacing now via card margin.
  - **#36 (LOW)** api/client.ts mergeSignals attached an abort listener to the external signal but cleanup only cleared the timeout, leaking listeners over poll cycles. Now removes the listener in cleanup.
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) performance pass — Round 5
- **Updated synthesis page**: mobile-app-performance (sections 29–32)
- **Key changes (code)**:
  - store/index.ts: RequestGuard now preserves the full ApiError instance (not just .message); store *Error fields typed as ApiError | null; non-ApiError throws wrapped as NETWORK
  - components/PageContainer.tsx: error prop typed as ApiError; CTA differentiates AUTH (🔑 "去配置 Key") vs other errors (⚠ "重试")
  - All 4 data screens (Dashboard/Models/Providers/Logs): onRetry switches to goToSettings when error.code === 'AUTH', else retries fetch; Logs/Models/Providers navigation props now typed via BottomTabNavigationProp
  - DashboardScreen: extracted inline 10000 polling literal to POLL_INTERVAL_MS constant
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) performance pass — Round 4
- **Updated synthesis page**: mobile-app-performance (sections 24–28, updated file map)
- **Key changes (code)**:
  - api/client.ts: added 15s request timeout (mergeSignals) + ApiError class with code field (TIMEOUT/NETWORK/AUTH/SERVER/CLIENT) for structured error handling
  - DashboardScreen: extracted inline keyExtractor arrows to stable useCallback refs; replaced `navigation: any` with typed `BottomTabNavigationProp`
  - New components/EmptyState.tsx (React.memo) — replaces 4 duplicated empty-state blocks across Dashboard/Models/Providers/Logs
  - LogsScreen: extracted magic numbers (LOG_FETCH_LIMIT, POLL_INTERVAL_MS) to named constants; retry reuses the stable fetch callback
  - Exported ApiError from api barrel
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) performance pass — Round 3
- **Updated synthesis page**: mobile-app-performance (sections 17–23, updated file map, trimmed follow-ups)
- **Key changes (code)**:
  - build.gradle: enabled R8 minify + shrinkResources by default for release builds (APK size reduction)
  - ProvidersScreen: replaced manual ScrollView grid with FlashList numColumns=2 (virtualization)
  - Extracted ModelCard, LogEntryItem, ProviderCard into React.memo components under components/{models,logs,providers}/
  - PageContainer: added onRetry prop → error state now shows a retry button; all 4 data screens wire it up
  - App.tsx: deduplicated StatusBar into a single top-level instance
  - TabNavigator: extracted screenOptions to module level (stable reference, fewer re-renders)
- **Validation**: `npx tsc --noEmit` passes clean

## [2026-06-29] synthesis | Mobile app (React Native) performance pass — Round 2
- **Updated synthesis page**: mobile-app-performance (sections 10–16 + updated file map + closed follow-ups)
- **Key changes (code)**:
  - Store: `RequestGuard` class with monotonic generation counter — only the latest response writes to the store, killing polling race conditions
  - Store: each `fetch*` action creates an AbortController and passes signal to the API client
  - New `src/hooks/usePolling.ts` — ref-based polling hook; all 4 list screens migrated off manual setInterval
  - New `src/components/ItemSeparator.tsx` — memoized module-level separator (replaces inline arrow `ItemSeparatorComponent`)
  - SettingsScreen: setTimeout stored in useRef, cleared on re-tap and on unmount
  - LogsScreen: keyExtractor appends `|${index}` to break ties on identical log entries
  - DashboardScreen: removed dead `status` variable and unused useEffect import
- **Validation**: `npx tsc --noEmit` passes clean

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
