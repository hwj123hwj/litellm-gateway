import { useEffect, useCallback, useMemo } from 'react'
import { View, Text, StyleSheet, TouchableOpacity } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { PageContainer, KpiCard, CardPanel, PageHeader } from '../components'
import { Colors, Typography, Spacing, Radius, Shadow, getProviderColor, getStatusColor } from '../theme'
import type { ProviderInfo, ModelInfo } from '../api'

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}

function formatLatency(ms: number): string {
  return ms < 1000 ? ms.toFixed(0) + 'ms' : (ms / 1000).toFixed(1) + 's'
}

export default function DashboardScreen({ navigation }: any) {
  const {
    dashboard,
    dashboardLoading,
    dashboardError,
    fetchDashboard,
    health,
    fetchHealth,
  } = useStore()

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

  // Provider 横向列表数据
  const providerData = useMemo(
    () => dashboard?.providers ?? [],
    [dashboard?.providers],
  )

  // 模型列表数据
  const modelData = useMemo(
    () =>
      (dashboard?.models ?? [])
        .sort((a, b) => b.requests - a.requests)
        .slice(0, 5),
    [dashboard?.models],
  )

  // 渲染 Provider 芯片
  const renderProviderChip = useCallback(
    ({ item }: { item: ProviderInfo }) => {
      const sc = getStatusColor(item.status)
      return (
        <TouchableOpacity
          style={styles.chip}
          onPress={() => navigation.navigate('ProvidersTab')}
          activeOpacity={0.7}
        >
          <View style={[styles.chipDot, { backgroundColor: sc.text }]} />
          <Text style={styles.chipLabel}>{item.name}</Text>
          <View style={[styles.chipBadge, { backgroundColor: sc.bg }]}>
            <Text style={[styles.chipBadgeText, { color: sc.text }]}>
              {formatLatency(item.avg_latency)}
            </Text>
          </View>
        </TouchableOpacity>
      )
    },
    [navigation],
  )

  // 渲染模型项
  const renderModelItem = useCallback(
    ({ item }: { item: ModelInfo }) => {
      const color = getProviderColor(item.provider || item.model)
      const sc = getStatusColor(item.status)
      return (
        <TouchableOpacity
          style={styles.modelItem}
          onPress={() => navigation.navigate('ModelsTab')}
          activeOpacity={0.7}
        >
          <View style={[styles.modelIcon, { backgroundColor: color }]}>
            <Text style={styles.modelIconText}>
              {item.model.slice(0, 2).toUpperCase()}
            </Text>
          </View>
          <View style={styles.modelInfo}>
            <Text style={styles.modelName}>{item.model}</Text>
            <Text style={styles.modelMeta}>
              {item.provider || '—'} · {formatNumber(item.requests)} 请求
            </Text>
          </View>
          <View style={[styles.modelDot, { backgroundColor: sc.text }]} />
          <Text style={styles.modelLatency}>
            {formatLatency(item.avg_latency)}
          </Text>
        </TouchableOpacity>
      )
    },
    [navigation],
  )

  if (!dashboard && dashboardLoading) {
    return <PageContainer loading />
  }

  if (dashboardError) {
    return <PageContainer error={dashboardError} />
  }

  const summary = dashboard?.summary

  return (
    <FlashList
      data={[]} // 顶层用空数据驱动 header/footer
      renderItem={() => null}
      ListHeaderComponent={
        <View style={styles.container}>
          <PageHeader title="Dashboard" />

          {/* Status row */}
          {summary && (
            <View style={styles.statusRow}>
              <View style={styles.pulse} />
              <Text style={styles.statusText}>
                {summary.today_requests > 0
                  ? '所有系统正常运行'
                  : '等待请求...'}
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
                trend={
                  summary.avg_latency_ms < 2000 ? '→ 正常' : '↓ 偏高'
                }
                trendType={
                  summary.avg_latency_ms < 2000 ? 'up' : 'down'
                }
              />
            </View>
          )}

          {/* Provider Strip */}
          {providerData.length > 0 && (
            <FlashList
              data={providerData}
              renderItem={renderProviderChip}
              keyExtractor={(item) => item.name}
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={styles.stripPadding}
            />
          )}

          {/* 活跃模型 */}
          <CardPanel
            title="活跃模型"
            action="查看全部 →"
            onAction={() => navigation.navigate('ModelsTab')}
          >
            {modelData.length === 0 ? (
              <View style={styles.empty}>
                <Text style={styles.emptyIcon}>📭</Text>
                <Text style={styles.emptyText}>
                  暂无模型数据，等待第一个请求...
                </Text>
              </View>
            ) : (
              <FlashList
                data={modelData}
                renderItem={renderModelItem}
                keyExtractor={(item) => item.model}
                scrollEnabled={false}
              />
            )}
          </CardPanel>

          {/* 用量排行表格 */}
          {dashboard && dashboard.models.length > 0 && (
            <CardPanel title="模型用量排行">
              {/* 表头 */}
              <View style={styles.tableHeader}>
                <Text style={[styles.th, { flex: 2 }]}>模型</Text>
                <Text style={[styles.th, { flex: 1 }]}>请求</Text>
                <Text style={[styles.th, { flex: 1 }]}>Tokens</Text>
              </View>
              {dashboard.models
                .sort((a, b) => b.requests - a.requests)
                .slice(0, 5)
                .map((m) => {
                  const maxReq = Math.max(
                    ...dashboard.models.map((x) => x.requests),
                    1,
                  )
                  const pct = (m.requests / maxReq) * 100
                  const color = getProviderColor(m.provider || m.model)
                  return (
                    <View key={m.model} style={styles.tableRow}>
                      <View style={[styles.td, { flex: 2 }]}>
                        <View
                          style={[styles.tableDot, { backgroundColor: color }]}
                        />
                        <Text style={styles.tableModel} numberOfLines={1}>
                          {m.model}
                        </Text>
                      </View>
                      <Text style={[styles.tdValue, { flex: 1 }]}>
                        {formatNumber(m.requests)}
                      </Text>
                      <Text style={[styles.tdValue, { flex: 1 }]}>
                        {formatNumber(m.total_tokens)}
                      </Text>
                    </View>
                  )
                })}
            </CardPanel>
          )}
        </View>
      }
    />
  )
}

const styles = StyleSheet.create({
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
  stripPadding: {
    paddingHorizontal: Spacing[1],
    paddingVertical: Spacing[1],
  },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
    paddingHorizontal: Spacing[4],
    paddingVertical: Spacing[3],
    backgroundColor: Colors.card,
    borderRadius: Radius.full,
    borderWidth: 1,
    borderColor: Colors.border,
    marginRight: Spacing[2],
    ...Shadow.card,
  },
  chipDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  chipLabel: {
    fontSize: Typography.fontSize.base,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    fontFamily: Typography.fontFamily.body,
  },
  chipBadge: {
    paddingHorizontal: Spacing[1],
    paddingVertical: 2,
    borderRadius: 6,
  },
  chipBadgeText: {
    fontSize: 10,
    fontWeight: Typography.fontWeight.bold,
    fontFamily: Typography.fontFamily.body,
  },
  // Model list
  modelItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[3],
    paddingVertical: Spacing[3],
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  modelIcon: {
    width: 40,
    height: 40,
    borderRadius: Radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  modelIconText: {
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.bold,
    color: '#fff',
    fontFamily: Typography.fontFamily.body,
  },
  modelInfo: {
    flex: 1,
  },
  modelName: {
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    fontFamily: Typography.fontFamily.body,
  },
  modelMeta: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  modelDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  modelLatency: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontWeight: Typography.fontWeight.medium,
    fontFamily: Typography.fontFamily.body,
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
  tableRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: Spacing[3],
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  td: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
  },
  tdValue: {
    fontSize: Typography.fontSize.base,
    color: Colors.text,
    fontFamily: Typography.fontFamily.body,
  },
  tableDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  tableModel: {
    fontSize: Typography.fontSize.base,
    color: Colors.text,
    flex: 1,
    fontFamily: Typography.fontFamily.body,
  },
})
