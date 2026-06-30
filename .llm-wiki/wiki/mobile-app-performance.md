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

## Round 6 — Regression & bug audit (2026-06-29)

A review of the 5 preceding optimization commits surfaced several issues introduced by the refactors themselves. This round fixes them.

### 33. RequestGuard never aborted the in-flight request (HIGH)
- **Bug**: R2's `RequestGuard` created an `AbortController` for each `run()` call and passed its signal to the fetcher, but **never called `.abort()` on the previous controller** when a new request started. The generation counter only prevented stale writes to the store — the old network request still ran to completion, wasting bandwidth and socket connections. The "race protection" was half-functional: it guarded *state* but not *resources*.
- **Fix**: `RequestGuard` now holds a `currentController` ref. On each new `run()`, it calls `currentController.abort()` before creating a new one. The `finally` block clears the ref so we don't repeatedly abort a completed request. Now a new poll genuinely cancels the previous in-flight fetch.

### 34. DashboardScreen subscribed to `health` state but never used it (HIGH)
- **Bug**: `const health = useStore((s) => s.health)` was in `DashboardScreen`, but the value was only used for `const status = health?.status || 'unknown'` — and `status` itself was removed as dead code in R2 (item #16). The `useStore` subscription remained, meaning **every 10s health poll caused a full DashboardScreen re-render** for nothing, since `health` data was fetched but the render output didn't depend on it.
- **Fix**: removed the `health` selector entirely. `DashboardScreen` now only subscribes to `fetchHealth` (a stable function reference) and calls it in the polling callback, without subscribing to the health *data*.

### 35. ProvidersScreen `numColumns=2` + `ItemSeparatorComponent` layout bug (MEDIUM)
- **Bug**: R3 switched ProvidersScreen to `FlashList numColumns={2}` but kept `ItemSeparatorComponent={ItemSeparator}`. With multiple columns, FlashList/FlatList inserts separators **between horizontal neighbors too**, producing a 12px gap *inside* each row between the two cards, on top of the vertical gap between rows. The grid looked uneven.
- **Fix**: removed `ItemSeparatorComponent` from the ProvidersScreen FlashList. Card spacing is now handled by `margin: Spacing[2]` + `flex: 1` inside `ProviderCard`'s `card` style, giving uniform gutters in both directions.

### 36. `mergeSignals` event-listener leak (LOW)
- **Bug**: R4's `mergeSignals` attached an `abort` listener to the external signal via `external.addEventListener('abort', ...)`, but the `cleanup` function only cleared the timeout timer — it never removed the listener. Over many poll cycles this leaked listener callbacks on the (long-lived) external AbortSignal.
- **Fix**: `cleanup` now captures the handler reference and calls `external.removeEventListener('abort', handler)`.

## Round 7 — Second regression & bug audit (2026-06-29)

A deeper review of the code after R6 found 5 more issues, including dead code, UI inconsistencies, and robustness gaps.

### 37. `ModelRankRow` accepted `maxRequests` prop but never used it (MEDIUM)
- **Bug**: The component was designed to show a progress bar (needing `maxRequests` for relative width), but the implementation rendered only text values. Despite this, `DashboardScreen` ran a `useMemo` to compute `maxRequests` and passed it as a prop — triggering unnecessary `ModelRankRow` re-renders whenever `maxRequests` changed.
- **Fix**: Removed `maxRequests` from both the `ModelRankRowProps` interface and the JSX in `DashboardScreen`. Deleted the unused `useMemo` for `maxRequests`.

### 38. `ProviderCard` rendered raw `item.requests` instead of `formatNumber()` (MEDIUM)
- **Bug**: Every other card/chip in the app used `formatNumber()` (e.g., `1500` → `"1.5K"`), but `ProviderCard` rendered `{item.requests}` as a raw number. This caused visual inconsistency — large request counts overflowed the narrow grid card.
- **Fix**: Imported `formatNumber` and applied it. Now consistent with `ProviderChip`, `ModelCard`, and `LogEntryItem`.

### 39. `fetchJSON` did not catch `res.json()` parse failures (LOW)
- **Bug**: If the server returned a 200 OK but a non-JSON body (e.g., an HTML error page from a reverse proxy, or an empty body), `res.json()` threw a raw `SyntaxError`. This was not caught by the network `catch` block, so it propagated as an unhandled exception — eventually wrapped as a misleading `NETWORK` error by `RequestGuard`.
- **Fix**: Wrapped `res.json()` in `try/catch`, throwing `new ApiError('服务器返回的数据格式错误', 'SERVER')` on failure. The user now sees an accurate "server returned malformed data" message.

### 40. `RequestGuard` had unreachable `AbortError` check (dead code) (MEDIUM)
- **Bug**: R4's `fetchJSON` already wraps **all** `AbortError`s into `ApiError('TIMEOUT')`. By the time the error reaches `RequestGuard`, it is always an `ApiError`, never a raw `AbortError`. The `if (e?.name === 'AbortError') return` guard was dead code — it could never be true.
- **Fix**: Removed the dead `AbortError` check. Replaced it with a clearer comment explaining that cancelled (superseded) requests are handled implicitly: `gen !== this.generation` means the error belongs to a stale request and is naturally ignored.

### 41. DashboardScreen polled `fetchHealth` but never used health data (MEDIUM)
- **Bug**: DashboardScreen called both `fetchDashboard()` and `fetchHealth()` every 10s. However, after R6 removed the `health` state subscription, the health data fetched by this poll was never consumed by any UI component — it just triggered unnecessary store updates and network traffic.
- **Fix**: Removed `fetchHealth()` from the DashboardScreen polling callback. Health polling is now solely the responsibility of a dedicated health indicator (if one is ever added). `fetchHealth` remains in the store for future use but is no longer wasted on the Dashboard.

## Round 8 — Third regression & bug audit (2026-06-29)

A code-analysis sub-agent performed a deep review of all files. This round addresses the findings.

### 42. `usePolling` lacked debounce — rapid tab switches stacked requests (MEDIUM)
- **Bug**: `usePolling` ran `fn` on every interval tick unconditionally. On slow networks, if a poll cycle elapsed before the previous fetch resolved, a new request was initiated while the old one was still in-flight. `RequestGuard` would abort the old one, but the abort-then-refetch cycle was wasteful and could cause UI flicker.
- **Fix**: `usePolling` now tracks an `isRunning` ref. If the previous `fn()` promise hasn't settled, the tick is skipped entirely. This provides debounce at the source, reducing unnecessary network initiations before `RequestGuard` even needs to abort.

### 43. `RequestGuard.set` bypassed TypeScript safety with string-keyed records (HIGH)
- **Bug**: `RequestGuard` received the zustand `set` function typed as `(partial: Partial<Record<string, unknown>>) => void` and constructed keys dynamically (e.g., `[this.keys.data]`). This erased all type safety — a typo in `dataKey` (e.g. `'dashbord'`) would compile silently and write to a non-existent store field, with the real field never updating.
- **Fix**: Refactored `RequestGuard` to be generic over a prefix `K` (e.g. `'dashboard'`). It now constructs keys as template literals (`${this.prefix}`, `${this.prefix}Loading`, `${this.prefix}Error`) and casts to `Partial<AppState>` so the compiler validates that the derived keys exist on `AppState`. A typo like `'dashbord'` now fails at compile time. Also corrected the custom `StoreSet` type to match zustand's actual `set` signature (removed the incompatible `replace` parameter).

### 44. FlashList v2 does not accept `estimatedItemSize` (MEDIUM, build-breaking)
- **Bug**: The type definitions for `@shopify/flash-list@2.0.2` do not declare `estimatedItemSize` (it was deprecated/removed in the v2 API). Adding `estimatedItemSize={N}` to any FlashList caused a `tsc` error: "Property 'estimatedItemSize' does not exist on type 'FlashListProps'". This was a latent build-breaking issue that would fail any CI type-check step.
- **Fix**: Removed `estimatedItemSize` from all three FlashList instances (Logs, Models, Providers). FlashList v2 handles sizing automatically.

## Round 9 — Fourth audit: initial-state & URL construction bugs (2026-06-29)

### 45. SetupScreen initialized URL with a broken `'http://'` prefix (HIGH)
- **Bug**: `App.tsx` `SetupScreen` used `useState('http://')` as the initial value for the backend URL input. If a user clicked "开始使用" without editing, the store would be set to `http://` — a malformed URL with no host. Every subsequent API request would fail with a `NETWORK` error, and since the URL is "set" (non-empty), the app would skip the setup screen on next launch, leaving the user stuck in a broken state with no obvious recovery.
- **Fix**: Changed initial value to `useState('')` (empty string). The `handleStart` callback already guards with `if (url.trim())`, so an empty submit no longer writes a broken URL. If left empty, the app falls through to the default `http://10.0.2.2:4001/admin` dev address.

### 46. `getBaseUrl()` unconditionally appended `/admin` (MEDIUM)
- **Bug**: `client.ts` `getBaseUrl()` always appended `/admin` to the configured URL. If a user pasted `http://server:4001/admin` (a natural thing to do), the result was `http://server:4001/admin/admin`, causing all requests to 404. This is a common failure mode for self-hosted tools where the admin path is part of the base URL.
- **Fix**: Added a regex check (`/\/admin\/?$/i`) — if the user's URL already ends with `/admin`, it is used as-is; otherwise `/admin` is appended. Prevents double-pathing.

### 47. `getLogs` default `limit` mismatch between client and store (LOW)
- **Bug**: `client.ts getLogs` had a default `limit=50`, but `store/index.ts fetchLogs` had a default `limit=100`. Since the store always passes its default explicitly, the client default was never used in practice — but the inconsistency was a latent trap: if any future code called `getLogs()` directly (without going through the store), it would silently fetch half the expected logs.
- **Fix**: Aligned both defaults to `100`.

## Round 10 — Cleartext HTTP traffic blocked by Android Network Security Policy (2026-06-29)

### 48. `CLEARTEXT communication not permitted by network security policy` (HIGH, runtime crash)
- **Bug**: The app targets Android 9+ (API 28+), where cleartext (HTTP) traffic is blocked by default. The app lets users configure a custom backend URL — typically an IP or domain over HTTP (e.g. `http://8.141.97.21:4001`). When the app tried to fetch, the OS blocked the request with `java.net.UnknownServiceException: CLEARTEXT communication to 8.141.97.21 not permitted by network security policy`. The `debug` build variant had `usesCleartextTraffic="true"`, but the `main` (release) manifest did not — so release APKs were completely broken for any HTTP backend.
- **Fix**:
  1. Added `android:usesCleartextTraffic="true"` to the `<application>` tag in `main/AndroidManifest.xml`.
  2. Created a dedicated `network_security_config.xml` that explicitly permits cleartext traffic, and referenced it via `android:networkSecurityConfig="@xml/network_security_config"`. This is the Android-recommended approach and works across all build variants (debug + release).

## Round 11 — Setup screen usability & broken initial state recovery (2026-06-29)

### 49. Stuck on broken state due to invalid `http://` URL persisted in storage (HIGH)
- **Bug**: Users who had installed v1.0.0 and saved the old `'http://'` default still had that broken URL in AsyncStorage. After upgrading, the app loaded `'http://'`, which is truthy (non-empty), so `showSetup = initialized && !backendUrl` was `false`. The app jumped to the main screen with a broken URL, showing only "NETWORK" errors with no way back to setup.
- **Fix**: Replaced the truthy check with `isValidUrl()` using `new URL(url)`. A URL like `'http://'` has no host and now correctly triggers the setup screen. Self-healing on next launch.

### 50. Setup screen lacks quick-fill for known backends (MEDIUM)
- **Bug**: Users had to manually type the IP and port on a mobile keyboard.
- **Fix**: Added a "快捷填充" row with a "生产服务器" button that auto-fills `http://8.141.97.21:4001`.

### 51. Bump version to 1.2.0
- Bumped `app.json` version `1.1.1` → `1.2.0` for the setup-screen UX overhaul and self-healing URL validation.

## Round 12 — Cleartext HTTP config lost by `expo prebuild` (2026-06-29)

### 52. `usesCleartextTraffic` overwritten by `expo prebuild` (HIGH, build-breaking)
- **Bug**: R10 manually added `android:usesCleartextTraffic="true"` and `network_security_config.xml` directly to the `android/` directory. However, the CI workflow runs `npx expo prebuild --platform android` *before* building, which **regenerates the entire `android/` directory** from `app.json`. This wiped out the manual changes, causing the v1.2.0 release APK to ship without cleartext permission — users still got `CLEARTEXT communication not permitted` errors.
- **Fix**: Created an Expo Config Plugin (`app.plugin.js`) that programmatically injects `android:usesCleartextTraffic="true"` and generates `res/xml/network_security_config.xml` during the `prebuild` phase. Registered it in `app.json` via `"plugins": ["./app.plugin.js"]`. This ensures the cleartext config survives `prebuild` regeneration and is applied consistently across all builds.

### 53. Bump version to 1.2.1
- Bumped version for the config plugin fix.

## Round 13 — CI build pipeline: prebuild override + JS syntax fix (2026-06-29)

### 54. `expo prebuild` overwrites manual native config (HIGH)
- **Bug**: R10 manually edited `android/` files for cleartext HTTP. But `expo prebuild` (run in CI) regenerates the entire `android/` directory, discarding manual changes. APKs were built without cleartext permission.
- **Fix**: Added a `Patch Android Manifest for Cleartext HTTP` step in the CI workflow that runs *after* `expo prebuild`. Uses `sed` to inject `usesCleartextTraffic` + `networkSecurityConfig` into the `<application>` tag, and `printf` to generate `network_security_config.xml` reliably (heredoc had indentation issues).

### 55. Duplicate closing brace in `client.ts` caused JS bundle SyntaxError (HIGH)
- **Bug**: R12's edit left an extra `}` after `getBaseUrl()`. Metro bundler rejected the bundle with `SyntaxError: Unexpected token (34:0)`, failing `:app:createBundleReleaseJsAndAssets`. `tsc --noEmit` didn't catch it (extra `}` at module level is syntactically valid JS but invalid for the parser position).
- **Fix**: Removed the duplicate brace.

### 56. Version bumps 1.2.1 → 1.2.5
- Multiple iterations to fix CI issues.

## Round 14 — Bottom tab bar occluded by system nav (2026-06-29)

### 57. Bottom tab buttons hidden behind Android gesture navigation bar (UI)
- **Bug**: The 5 bottom tab buttons (首页/模型/提供商/日志/设置) were nearly hidden behind the system gesture navigation bar on the user's device.
- **Root cause**: `TabNavigator` used a fixed `height: 64px` + `paddingBottom: 8px` in `tabBarStyle`. This didn't account for the device's bottom safe area inset. On phones with gesture navigation (no hardware buttons), the system navigation bar overlaps the bottom of the app, occluding the tab labels and icons.
- **Fix**: `TabNavigator` now calls `useSafeAreaInsets()` to dynamically compute the bottom inset. The tab bar height is now `baseHeight + bottomInset` and `paddingBottom` is `bottomInset + 6`. This ensures the tabs sit above the system navigation bar on all devices (Android gesture nav, Android 3-button nav, iOS home indicator).
- Also added `tabBarItemStyle: { paddingVertical: 4 }` for better touch target sizing.

## Follow-ups (remaining)

These items from the RN best-practices skill are still open; apply only when a measured problem exists:

- **React Compiler**: enable `babel-plugin-react-compiler` for automatic memoization if profiler shows wasted renders after the selector + memo refactor.
- **FlashList `estimatedItemSize`**: the @shopify/flash-list v2 type defs in this project do not declare `estimatedItemSize` (it is deprecated in v2), so it was intentionally omitted. If upgrading to a version that re-introduces it, add it back for accurate recycling.
- **Bundle analysis**: run `npx react-native bundle ... + source-map-explorer` to baseline JS bundle size before/after further changes.
- **Hermes mmap**: `enableBundleCompression` is already `false` in `build.gradle` and `expo.useLegacyPackaging=false` in `gradle.properties`, so the JS bundle should be uncompressed/mmap-able. Verify with an actual release APK build.

## Related

- [[overview]] — Gateway architecture and admin UI overview
- [[admin-dashboard]] — Web (React + Capacitor) variant of the admin dashboard
