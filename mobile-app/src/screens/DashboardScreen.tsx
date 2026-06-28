import { useEffect, useCallback, useMemo } from 'react'
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ScrollView,
} from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { PageContainer, KpiCard, CardPanel, PageHeader } from '../components'
import {
  ProviderChip,
  ActiveModelItem,
  ModelRankRow,
} from '../components/dashboard'
import { Colors, Typography, Spacing, Radius } from '../theme'
import { formatNumber, formatLatency } from '../utils'
import type { ModelInfo } from '../api'

export default function DashboardScreen({ navigation }: any) {
  // 使用精确选择器，避免 store 任意字段变化导致的重渲染
  const dashboard = useStore((s) => s.dashboard)
  const dashboardLoading = useStore((s) => s.dashboardLoading)
  const dashboardError = useStore((s) => s.dashboardError)
  const fetchDashboard = useStore((s) => s.fetchDashboard)
  const health = useStore((s) => s.health)
  const fetchHealth = useStore((s) => s.fetchHealth)

  // 轮询数据 - 在 effect 中启动和清理
  useEffect(() => {
    fetchDashboard()
    fetchHealth()
    const timer = setInterval(() => {
      fetchDashboard()
      fetchHealth()
    }, 10000)
    return () => clearInterval(timer)
  }, [fetchDashboard, fetchHealth])

  const status = health?.status || 'unknown'

  // 预排序 & 切片的模型列表，避免在 render 中重复排序
  const sortedModels = useMemo(() => {
    const models = dashboard?.models ?? []
    return [...models].sort((a, b) => b.requests - a.requests)
  }, [dashboard?.models])

  const topModels = useMemo(() => sortedModels.slice(0, 5), [sortedModels])
  const providerData = useMemo(() => dashboard?.providers ?? [], [
    dashboard?.providers,
  ])

  // 最大请求数用于进度条计算，避免每行都重新计算
  const maxRequests = useMemo(() => {
    return Math.max(...sortedModels.map((x) => x.requests), 1)
  }, [sortedModels])

  // 稳定的导航回调
  const goToProviders = useCallback(
    () => navigation.navigate('ProvidersTab'),
    [navigation],
  )
  const goToModels = useCallback(
    () => navigation.navigate('ModelsTab'),
    [navigation],
  )

  const renderProviderChip = useCallback(
    ({ item }: { item: typeof providerData[number] }) => (
      <ProviderChip item={item} onPress={goToProviders} />
    ),
    [goToProviders],
  )

  const renderModelItem = useCallback(
    ({ item }: { item: ModelInfo }) => (
      <ActiveModelItem item={item} onPress={goToModels} />
    ),
    [goToModels],
  )

  if (!dashboard && dashboardLoading) {
    return <PageContainer loading />
  }

  if (dashboardError && !dashboard) {
    return <PageContainer error={dashboardError} />
  }

  const summary = dashboard?.summary

  return (
    <ScrollView
      style={styles.scroll}
      contentContainerStyle={styles.container}
      showsVerticalScrollIndicator={false}
      keyboardShouldPersistTaps="handled"
    >
      <PageHeader title="Dashboard" />

      {/* Status row */}
      {summary && (
        <View style={styles.statusRow}>
          <View style={styles.pulse} />
          <Text style={styles.statusText}>
            {summary.today_requests > 0 ? '所有系统正常运行' : '等待请求...'}
          </Text>
          <Text style={styles.statusTime}>运行 {summary.uptime}</Text>
        </View>
      )}

      {/* KPI Grid */}
      {summary && (
        <View style={styles.kpiRow}>
          <KpiCard
            label="📊 今日请求"
            value={formatNumber(summary.today_requests)}
          />
          <KpiCard
            label="✅ 成功率"
            value={summary.success_rate.toFixed(1) + '%'}
            trend={
              summary.success_rate >= 99
                ? '↑ 优秀'
                : summary.success_rate >= 95
                  ? '→ 正常'
                  : '↓ 偏低'
            }
            trendType={
              summary.success_rate >= 99
                ? 'up'
                : summary.success_rate >= 95
                  ? 'neutral'
                  : 'down'
            }
          />
        </View>
      )}
      {summary && (
        <View style={styles.kpiRow}>
          <KpiCard
            label="📦 活跃模型"
            value={String(summary.active_models)}
          />
          <KpiCard
            label="⏱ 平均延迟"
            value={formatLatency(summary.avg_latency_ms)}
            trend={summary.avg_latency_ms < 2000 ? '→ 正常' : '↓ 偏高'}
            trendType={summary.avg_latency_ms < 2000 ? 'up' : 'down'}
          />
        </View>
      )}

      {/* Provider Strip */}
      {providerData.length > 0 && (
        <View style={styles.stripWrap}>
          <FlashList
            data={providerData}
            renderItem={renderProviderChip}
            keyExtractor={(item) => item.name}
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={styles.stripPadding}
          />
        </View>
      )}

      {/* 活跃模型 */}
      <CardPanel
        title="活跃模型"
        action="查看全部 →"
        onAction={goToModels}
      >
        {topModels.length === 0 ? (
          <View style={styles.empty}>
            <Text style={styles.emptyIcon}>📭</Text>
            <Text style={styles.emptyText}>
              暂无模型数据，等待第一个请求...
            </Text>
          </View>
        ) : (
          <FlashList
            data={topModels}
            renderItem={renderModelItem}
            keyExtractor={(item) => item.model}
            scrollEnabled={false}
          />
        )}
      </CardPanel>

      {/* 用量排行表格 */}
      {topModels.length > 0 && (
        <CardPanel title="模型用量排行">
          {/* 表头 */}
          <View style={styles.tableHeader}>
            <Text style={[styles.th, { flex: 2 }]}>模型</Text>
            <Text style={[styles.th, { flex: 1 }]}>请求</Text>
            <Text style={[styles.th, { flex: 1 }]}>Tokens</Text>
          </View>
          {topModels.map((m) => (
            <ModelRankRow
              key={m.model}
              item={m}
              maxRequests={maxRequests}
            />
          ))}
        </CardPanel>
      )}
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  scroll: {
    flex: 1,
  },
  container: {
    padding: Spacing[4],
    gap: Spacing[4],
    paddingBottom: Spacing[16],
  },
  // Status row
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
    padding: Spacing[3],
    backgroundColor: Colors.greenLight,
    borderRadius: Radius.lg,
  },
  pulse: {
    width: 10,
    height: 10,
    borderRadius: 5,
    backgroundColor: Colors.green,
  },
  statusText: {
    flex: 1,
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.green,
    fontFamily: Typography.fontFamily.body,
  },
  statusTime: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  // KPI
  kpiRow: {
    flexDirection: 'row',
    gap: Spacing[3],
  },
  // Provider strip
  stripWrap: {
    marginHorizontal: -Spacing[4],
  },
  stripPadding: {
    paddingHorizontal: Spacing[4],
    paddingVertical: Spacing[1],
  },
  // Empty
  empty: {
    alignItems: 'center',
    paddingVertical: Spacing[10],
  },
  emptyIcon: {
    fontSize: 48,
    marginBottom: Spacing[3],
  },
  emptyText: {
    fontSize: Typography.fontSize.md,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  // Table
  tableHeader: {
    flexDirection: 'row',
    paddingBottom: Spacing[2],
    borderBottomWidth: 1,
    borderBottomColor: Colors.border,
    marginBottom: Spacing[2],
  },
  th: {
    fontSize: Typography.fontSize.sm,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.textMuted,
    textTransform: 'uppercase',
    fontFamily: Typography.fontFamily.body,
  },
})
