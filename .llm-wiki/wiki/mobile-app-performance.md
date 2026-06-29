---
type: concept
date: 2026-06-29
tags:
  - mobile-app
  - react-native
  - performance
  - best-practices
---

# Mobile App (React Native) — Performance Optimization

## Summary

The `mobile-app/` directory contains an Expo + React Native (SDK 56 / RN 0.85) administrative dashboard for the LiteLLM Gateway. On 2026-06-29 a first round of performance and correctness fixes was applied following the Callstack **React Native Best Practices** skill (`react-native-best-practices`).

## Tech Stack

- Expo SDK 56 (RN 0.85.3, React 19)
- React Navigation 7 (bottom tabs)
- Zustand 5 (state)
- @shopify/flash-list 2 (virtualized lists)
- react-native-reanimated 4 + gesture-handler 2
- TypeScript 6

## Problems Found & Fixes Applied (2026-06-29)

### 1. Anti-pattern: nested FlashList with empty data (`DashboardScreen`)
- **Before**: `<FlashList data={[]} renderItem={() => null} ListHeaderComponent={...} />` — using FlashList purely as a vertical ScrollView wrapper. This disables virtualization, adds an empty recycling layer, and nests horizontal FlashLists inside it (worse jank).
- **After**: replaced the outer FlashList with a plain `ScrollView`; horizontal chip strip + `scrollEnabled={false}` inner FlashList remain for actual list rendering.

### 2. Sort-in-render (`DashboardScreen`, `ModelsScreen`)
- **Before**: `.sort()` was called directly on the source array during render, mutating state in place and re-sorting every render.
- **After**: moved sorting into `useMemo` with a defensive copy (`[...arr].sort(...)`), precomputed top-N and `maxRequests` so list rows don't recompute.

### 3. In-place state mutation (`ModelsScreen`)
- **Before**: `(models?.models ?? []).sort(...)` mutated the array held by Zustand.
- **After**: `[...(models?.models ?? [])].sort(...)`.

### 4. Coarse Zustand selectors (all screens + `App.tsx`)
- **Before**: `const { a, b, c } = useStore()` subscribed the component to the entire store; any unrelated field change triggered a re-render. The RN skill flags broad store/context updates as a primary source of cascading re-renders.
- **After**: each screen now uses atomic selectors: `useStore((s) => s.dashboard)`, `useStore((s) => s.fetchDashboard)`, etc. Only fields actually consumed cause re-renders.

### 5. Duplicate polling & dangling timers (`App.tsx`)
- **Before**: `App.tsx` ran a 15s `fetchHealth` interval while `DashboardScreen` also polled health every 10s — double load + race. `showSetup` was an extra `useState` mirror of `initialized && !backendUrl`.
- **After**: removed the redundant interval from `App.tsx`; `DashboardScreen` is the single owner of health polling. `showSetup` is now derived state computed inline (no extra render cycle).

### 6. Unstable list keys (`LogsScreen`)
- **Before**: `keyExtractor={(_, i) => String(i)}` — index-based keys break FlashList/RecyclerListView recycling when items prepend/shift, causing full re-renders and lost item state.
- **After**: `keyExtractor` now returns `${timestamp}|${model}|${path}` for stable identity.

### 7. Duplicated helper functions
- **Before**: `formatNumber`, `formatLatency`, `formatTime`, `abbreviate` were copy-pasted across Dashboard / Models / Logs / Providers screens.
- **After**: extracted to `src/utils/format.ts` with barrel `src/utils/index.ts`; all screens import from `../utils`.

### 8. Monolithic inline render items
- **Before**: `renderProviderChip`, `renderModelItem`, `ModelRankRow` were closures defined inside `DashboardScreen`, recreated every render and impossible for FlashList to memoize.
- **After**: extracted into `src/components/dashboard/index.tsx` as `React.memo`-wrapped components (`ProviderChip`, `ActiveModelItem`, `ModelRankRow`), receiving stable `onPress` callbacks.

### 9. Request cancellation plumbing
- **Before**: `fetchJSON` had no way to be cancelled; in-flight requests could resolve after unmount and call `setState` on an unmounted component.
- **After**: `fetchJSON` and all `get*` API functions accept an optional `AbortSignal`. (Store actions wire it up in a follow-up.)

## Round 2 — Race conditions, hooks, and remaining anti-patterns (2026-06-29)

### 10. Polling race condition (store-level generation guard)
- **Before**: `fetchDashboard`/`fetchLogs`/etc. were plain async functions. Fast polling (10–15s intervals) or rapid tab switching could let a slow previous response overwrite a newer one (stale-write race).
- **After**: introduced a `RequestGuard` class in `store/index.ts`. Each data domain owns a guard with a monotonic generation counter. Only the response from the latest invocation writes to the store; older responses are discarded. `AbortError` is swallowed.

### 11. AbortController threaded through store actions
- **Before**: The store actions did not pass the `AbortSignal` (added to the API client in round 1) to the fetcher.
- **After**: Each guarded action creates an `AbortController` and passes its signal to the underlying `get*` call, so in-flight requests are abortable.

### 12. `usePolling` hook replaces manual `setInterval`
- **Before**: Every screen duplicated `useEffect(() => { fn(); const t = setInterval(fn, N); return () => clearInterval(t) }, [fn])`.
- **After**: extracted `src/hooks/usePolling.ts`. Uses a ref to always call the latest callback without restarting the interval. All five screens now use it.

### 13. Inline `ItemSeparatorComponent` (FlashList/FlatList anti-pattern)
- **Before**: `ItemSeparatorComponent={() => <View style={styles.separator} />}` in `ModelsScreen` and `LogsScreen` — a new arrow function identity on every render, defeating list memoization.
- **After**: extracted `src/components/ItemSeparator.tsx` as a `React.memo`-wrapped module-level component; both screens pass the stable reference.

### 14. `SettingsScreen` timer leak
- **Before**: `setTimeout(() => setSaved(false), 2000)` was never cleared. Rapid taps stacked timers; navigating away could fire `setState` on an unmounted component.
- **After**: timer stored in a `useRef`, cleared before scheduling a new one, and cleared in a `useEffect` cleanup on unmount.

### 15. `LogsScreen` key collision risk
- **Before**: key was `${timestamp}|${model}|${path}` — two log entries with identical fields (same model, path, same-second timestamp) collide, causing React reconciliation bugs.
- **After**: appended `|${index}` as a tie-breaker. Identity is still dominated by the stable fields; index only breaks exact ties.

### 16. Dead code removal
- Removed unused `status` variable and `useEffect` import from `DashboardScreen`.

## File Map (after round 2)

```
mobile-app/src/
├── api/
│   ├── client.ts          # +AbortSignal support on fetchJSON / get*
│   ├── types.ts
│   └── index.ts
├── components/
│   ├── dashboard/
│   │   └── index.tsx      # memoized ProviderChip / ActiveModelItem / ModelRankRow
│   ├── logs/              # (R3)
│   │   └── LogEntryItem.tsx
│   ├── models/            # (R3)
│   │   └── ModelCard.tsx
│   ├── providers/         # (R3)
│   │   └── ProviderCard.tsx
│   ├── CardPanel.tsx
│   ├── EmptyState.tsx     # NEW (R4): memoized empty placeholder
│   ├── ItemSeparator.tsx  # memoized list separator (R2)
│   ├── KpiCard.tsx
│   ├── PageContainer.tsx  # (R3): +onRetry error button
│   ├── PageHeader.tsx
│   ├── StatusBadge.tsx
│   └── index.ts
├── hooks/                 # (R2)
│   ├── usePolling.ts      # usePolling(fn, intervalMs)
│   └── index.ts
├── navigation/
│   ├── TabNavigator.tsx   # R3: module-level screenOptions
│   └── index.ts
├── screens/
│   ├── DashboardScreen.tsx    # ScrollView + memoized items + precomputed sorts + usePolling + retry + typed nav + stable keys + EmptyState
│   ├── LogsScreen.tsx         # stable keyExtractor + usePolling + ItemSeparator + LogEntryItem + retry + EmptyState + named constants
│   ├── ModelsScreen.tsx       # atomic selectors + non-mutating sort + usePolling + ItemSeparator + ModelCard + retry + EmptyState
│   ├── ProvidersScreen.tsx    # atomic selectors + usePolling + FlashList numColumns=2 + ProviderCard + retry + EmptyState
│   ├── SettingsScreen.tsx     # atomic selectors + timer cleanup
│   └── index.ts
├── store/
│   └── index.ts          # R2: RequestGuard race-condition protection + AbortController
├── theme/
│   ├── colors.ts
│   ├── spacing.ts
│   ├── typography.ts
│   └── index.ts
└── utils/                  # NEW
    ├── format.ts           # formatNumber / formatLatency / formatRelativeTime / abbreviate
    └── index.ts
```

## Round 3 — Build optimization, componentization & UX (2026-06-29)

### 17. R8 + resource shrinking (release build size)
- **Before**: `android.enableMinifyInReleaseBuilds` defaulted to `false`; `shrinkResources` defaulted to `'false'`. Release APKs shipped full native + bridge code.
- **After**: both now default to `'true'` in `app/build.gradle`. The existing `proguard-rules.pro` already keeps Reanimated + TurboModules, so R8 is safe to enable. This shrinks the release APK (native code + unused JS bridge classes).

### 18. ProvidersScreen: ScrollView → FlashList numColumns=2
- **Before**: `ProvidersScreen` manually chunked data into rows-of-2 and rendered inside a `ScrollView`. Every provider rendered at once (no virtualization); manual `rows` useMemo added complexity.
- **After**: replaced with a single `<FlashList numColumns={2}>`. FlashList handles the grid layout, recycling, and only mounts visible items.

### 19. ModelCard extracted (ModelsScreen)
- **Before**: the model card markup was an inline closure inside `renderItem`, recreated every render — FlashList could not memoize it.
- **After**: extracted to `src/components/models/ModelCard.tsx` as a `React.memo` component. `ModelsScreen` is now ~60 lines (was ~210).

### 20. LogEntryItem extracted (LogsScreen)
- **Before**: same inline-closure anti-pattern for log entries.
- **After**: extracted to `src/components/logs/LogEntryItem.tsx`, `React.memo`-wrapped. `LogsScreen` is now ~75 lines (was ~190).

### 21. ProviderCard extracted (ProvidersScreen)
- **Before**: `ProviderCard` was a function defined inside the screen file; not memoized.
- **After**: moved to `src/components/providers/ProviderCard.tsx`, `React.memo`-wrapped.

### 22. PageContainer error retry
- **Before**: the error state only showed a static banner — no way to recover without restarting the app.
- **After**: `PageContainer` accepts an optional `onRetry` callback; when provided, a "重试" (retry) button renders below the error banner. All four data screens now pass their fetch function as the retry handler.

### 23. App.tsx StatusBar dedup + TabNavigator stable options
- **Before**: `<StatusBar style="dark" />` was duplicated across all three render branches in `App.tsx`. `TabNavigator.screenOptions` was an inline arrow created every render.
- **After**: StatusBar lifted to the top level (single instance). `screenOptions` extracted to a module-level function in `TabNavigator.tsx`.

## Round 4 — Network resilience, type safety & DRY (2026-06-29)

### 24. Request timeout + structured error classification (api/client.ts)
- **Before**: `fetchJSON` had no timeout — a hung backend would leave the loading spinner spinning forever. Errors were thrown as plain `Error("API error: 404 Not Found")` strings, giving the UI no way to distinguish auth failures from network drops.
- **After**:
  - Added a 15s default timeout via `mergeSignals()` (combines an internal timeout `AbortController` with the caller's external signal).
  - Introduced `ApiError` class with a discriminated `code` field: `'TIMEOUT' | 'NETWORK' | 'AUTH' | 'SERVER' | 'CLIENT'`. Network-layer catches, 401/403, and 5xx are now mapped to user-friendly Chinese messages at the API boundary, so `PageContainer` can later show different retry/CTA actions per code.
  - Exported `ApiError` from the api barrel.

### 25. DashboardScreen inline `keyExtractor` arrows
- **Before**: the Provider strip and Active Models FlashList used inline `(item) => item.name` / `(item) => item.model` arrows — new function identity every render.
- **After**: extracted to stable `useCallback` refs (`providerKeyExtractor`, `modelKeyExtractor`).

### 26. `navigation: any` → typed navigation prop (DashboardScreen)
- **Before**: `DashboardScreen({ navigation }: any)` — no type safety on `.navigate()` calls.
- **After**: imported `BottomTabNavigationProp` and typed the prop as `BottomTabNavigationProp<Record<string, object | undefined>, 'DashboardTab'>`.

### 27. EmptyState component (DRY)
- **Before**: four screens (Dashboard, Models, Providers, Logs) each duplicated a 10-line `<View><Text>icon</Text><Text>msg</Text></View>` empty block plus its 3 style entries.
- **After**: extracted `src/components/EmptyState.tsx` (`React.memo`-wrapped, `icon` + `message` props). All four screens now use `<EmptyState icon="📄" message="..." />`. Removed ~40 lines of duplicated styles.

### 28. Magic numbers → named constants (LogsScreen)
- **Before**: `fetchLogs(100)` and `10000` appeared as inline literals.
- **After**: `LOG_FETCH_LIMIT = 100` and `POLL_INTERVAL_MS = 10_000` declared at module top. Retry handler now reuses the same `fetch` callback used by polling (stable identity).

## Round 5 — Structured errors end-to-end (2026-06-29)

### 29. Store preserves full `ApiError` object (not just message string)
- **Before**: `RequestGuard` stored `e.message` (a plain string) into `*Error`. The structured `code` field from R4's `ApiError` was lost at the store boundary, so the UI had no way to know *why* a request failed.
- **After**: `RequestGuard` now stores the full `ApiError` instance. Store `*Error` fields are typed `ApiError | null`. Non-ApiError throws are wrapped as `code: 'NETWORK'`.

### 30. `PageContainer` shows differentiated CTA per error code
- **Before**: error state always showed a generic "重试" button regardless of error type.
- **After**: reads `error.code`; `AUTH` errors show 🔑 icon + "去配置 Key" button label, all others show ⚠ + "重试". This gives the user actionable guidance instead of a dead-end retry.

### 31. AUTH-error auto-redirect to Settings tab (all 4 data screens)
- **Before**: a 401/403 left the user on a blank error screen with no way to fix the key.
- **After**: DashboardScreen, ModelsScreen, ProvidersScreen, LogsScreen each compute `onRetry={error?.code === 'AUTH' ? goToSettings : fetchFn}`. When the backend rejects the API key, the button now navigates to the Settings tab instead of pointlessly retrying. All screens now receive a typed `BottomTabNavigationProp` navigation prop (Logs/Models/Providers were previously untyped).

### 32. DashboardScreen polling interval → named constant
- **Before**: `10000` was an inline literal passed to `usePolling`.
- **After**: extracted to module-level `POLL_INTERVAL_MS = 10_000`, matching the pattern already used in Logs/Models/Providers screens.

## Follow-ups (remaining)

These items from the RN best-practices skill are still open; apply only when a measured problem exists:

- **React Compiler**: enable `babel-plugin-react-compiler` for automatic memoization if profiler shows wasted renders after the selector + memo refactor.
- **FlashList `estimatedItemSize`**: the @shopify/flash-list v2 type defs in this project do not declare `estimatedItemSize` (it is deprecated in v2), so it was intentionally omitted. If upgrading to a version that re-introduces it, add it back for accurate recycling.
- **Bundle analysis**: run `npx react-native bundle ... + source-map-explorer` to baseline JS bundle size before/after further changes.
- **Hermes mmap**: `enableBundleCompression` is already `false` in `build.gradle` and `expo.useLegacyPackaging=false` in `gradle.properties`, so the JS bundle should be uncompressed/mmap-able. Verify with an actual release APK build.

## Related

- [[overview]] — Gateway architecture and admin UI overview
- [[admin-dashboard]] — Web (React + Capacitor) variant of the admin dashboard
