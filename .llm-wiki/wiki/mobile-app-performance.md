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

## File Map (after refactor)

```
mobile-app/src/
├── api/
│   ├── client.ts          # +AbortSignal support on fetchJSON / get*
│   ├── types.ts
│   └── index.ts
├── components/
│   ├── dashboard/
│   │   └── index.tsx      # NEW: memoized ProviderChip / ActiveModelItem / ModelRankRow
│   ├── CardPanel.tsx
│   ├── KpiCard.tsx
│   ├── PageContainer.tsx
│   ├── PageHeader.tsx
│   ├── StatusBadge.tsx
│   └── index.ts
├── navigation/
│   ├── TabNavigator.tsx
│   └── index.ts
├── screens/
│   ├── DashboardScreen.tsx    # ScrollView + memoized items + precomputed sorts
│   ├── LogsScreen.tsx         # stable keyExtractor
│   ├── ModelsScreen.tsx       # atomic selectors + non-mutating sort
│   ├── ProvidersScreen.tsx    # atomic selectors
│   ├── SettingsScreen.tsx     # atomic selectors
│   └── index.ts
├── store/
│   └── index.ts
├── theme/
│   ├── colors.ts
│   ├── spacing.ts
│   ├── typography.ts
│   └── index.ts
└── utils/                  # NEW
    ├── format.ts           # formatNumber / formatLatency / formatRelativeTime / abbreviate
    └── index.ts
```

## Follow-ups (not yet applied)

These items from the RN best-practices skill are still open; apply only when a measured problem exists:

- **React Compiler**: enable `babel-plugin-react-compiler` for automatic memoization if profiler shows wasted renders after the selector refactor.
- **FlashList `estimatedItemSize`**: the @shopify/flash-list v2 type defs in this project do not declare `estimatedItemSize` (it is deprecated in v2), so it was intentionally omitted. If upgrading to a version that re-introduces it, add it back for accurate recycling.
- **Bundle analysis**: run `npx react-native bundle ... + source-map-explorer` to baseline JS bundle size before/after further changes.
- **Hermes mmap**: verify Android JS bundle is **not** compressed in the APK so Hermes can mmap it (TTI win). Check `android/app/build.gradle` `enableHermes` + packaging options.
- **R8**: confirm `minifyEnabled` / `shrinkResources` are on for release builds.
- **Store AbortController wiring**: pass `signal` from a ref into `fetchDashboard`/`fetchLogs`/etc. inside the polling `useEffect` so requests are cancelled on unmount.

## Related

- [[overview]] — Gateway architecture and admin UI overview
- [[admin-dashboard]] — Web (React + Capacitor) variant of the admin dashboard
