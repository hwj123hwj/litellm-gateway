import { useCallback, useMemo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import { PageContainer, PageHeader, ItemSeparator } from '../components'
import { Colors, Typography, Spacing, Radius, Shadow, getProviderColor, getStatusColor } from '../theme'
import { formatNumber, formatLatency } from '../utils'

export default function ModelsScreen() {
  const models = useStore((s) => s.models)
  const modelsLoading = useStore((s) => s.modelsLoading)
  const modelsError = useStore((s) => s.modelsError)
  const fetchModels = useStore((s) => s.fetchModels)

  usePolling(fetchModels, 15000)

  const sortedModels = useMemo(
    () =>
      [...(models?.models ?? [])].sort((a, b) => b.requests - a.requests),
    [models?.models],
  )

  const activeCount = useMemo(
    () => sortedModels.filter((m) => m.status !== 'idle').length,
    [sortedModels],
  )

  const renderItem = useCallback(({ item }: { item: typeof sortedModels[number] }) => {
    const color = getProviderColor(item.provider || item.model)
    const sc = getStatusColor(item.status)

    const statusLabel =
      item.status === 'online'
        ? '在线'
        : item.status === 'degraded'
          ? '降级'
          : item.status === 'offline'
            ? '离线'
            : '空闲'

    return (
      <View style={styles.card}>
        <View style={styles.header}>
          <View style={[styles.icon, { backgroundColor: color }]}>
            <Text style={styles.iconText}>
              {item.model.slice(0, 2).toUpperCase()}
            </Text>
          </View>
          <View style={styles.headerInfo}>
            <Text style={styles.modelName}>{item.model}</Text>
            <Text style={styles.provider}>{item.provider || '—'}</Text>
          </View>
          <View style={[styles.statusBadge, { backgroundColor: sc.bg }]}>
            <Text style={[styles.statusText, { color: sc.text }]}>
              {statusLabel}
            </Text>
          </View>
        </View>

        <View style={styles.stats}>
          <View style={styles.stat}>
            <Text style={styles.statValue}>{formatNumber(item.requests)}</Text>
            <Text style={styles.statLabel}>请求</Text>
          </View>
          <View style={styles.stat}>
            <Text style={styles.statValue}>
              {formatNumber(item.total_tokens)}
            </Text>
            <Text style={styles.statLabel}>Tokens</Text>
          </View>
          <View style={styles.stat}>
            <Text style={styles.statValue}>
              {formatLatency(item.avg_latency)}
            </Text>
            <Text style={styles.statLabel}>延迟</Text>
          </View>
        </View>
      </View>
    )
  }, [])

  return (
    <PageContainer loading={modelsLoading && !models} error={modelsError}>
      <View style={styles.container}>
        <PageHeader
          title="模型管理"
          subtitle={`共 ${models?.total ?? 0} 个模型，${activeCount} 个活跃`}
        />
      </View>

      {sortedModels.length === 0 && !modelsLoading ? (
        <View style={styles.empty}>
          <Text style={styles.emptyIcon}>📭</Text>
          <Text style={styles.emptyText}>暂无模型数据</Text>
        </View>
      ) : (
        <FlashList
          data={sortedModels}
          renderItem={renderItem}
          keyExtractor={(item) => item.model}
          contentContainerStyle={styles.listContent}
          ItemSeparatorComponent={ItemSeparator}
        />
      )}
    </PageContainer>
  )
}

const styles = StyleSheet.create({
  container: {
    paddingHorizontal: Spacing[4],
    paddingTop: Spacing[4],
  },
  listContent: {
    paddingHorizontal: Spacing[4],
    paddingBottom: Spacing[16],
  },
  card: {
    backgroundColor: Colors.card,
    borderRadius: Radius.lg,
    borderWidth: 1,
    borderColor: Colors.border,
    padding: Spacing[4],
    ...Shadow.card,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[3],
    marginBottom: Spacing[3],
  },
  icon: {
    width: 40,
    height: 40,
    borderRadius: Radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  iconText: {
    fontSize: Typography.fontSize.base,
    fontWeight: Typography.fontWeight.bold,
    color: '#fff',
    fontFamily: Typography.fontFamily.body,
  },
  headerInfo: {
    flex: 1,
  },
  modelName: {
    fontSize: Typography.fontSize.lg,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    fontFamily: Typography.fontFamily.body,
  },
  provider: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  statusBadge: {
    paddingHorizontal: Spacing[3],
    paddingVertical: 3,
    borderRadius: Radius.full,
  },
  statusText: {
    fontSize: Typography.fontSize.xs,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
  stats: {
    flexDirection: 'row',
  },
  stat: {
    flex: 1,
    alignItems: 'center',
  },
  statValue: {
    fontSize: Typography.fontSize['2xl'],
    fontWeight: Typography.fontWeight.bold,
    color: Colors.text,
    fontFamily: Typography.fontFamily.heading,
  },
  statLabel: {
    fontSize: Typography.fontSize.xs,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  empty: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: Spacing[16],
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
})
