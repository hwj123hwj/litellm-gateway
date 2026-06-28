import { useMemo } from 'react'
import { View, Text, StyleSheet, ScrollView } from 'react-native'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import { PageContainer, PageHeader } from '../components'
import {
  Colors,
  Typography,
  Spacing,
  Radius,
  Shadow,
  getProviderColor,
  getStatusColor,
} from '../theme'
import { formatLatency, abbreviate } from '../utils'
import type { ProviderInfo } from '../api'

function ProviderCard({ item }: { item: ProviderInfo }) {
  const color = getProviderColor(item.name)
  const sc = getStatusColor(item.status)

  const statusLabel =
    item.status === 'online'
      ? '● 在线'
      : item.status === 'degraded'
        ? '● 降级'
        : item.status === 'offline'
          ? '● 离线'
          : '● 未知'

  return (
    <View style={styles.card}>
      <View style={[styles.icon, { backgroundColor: color }]}>
        <Text style={styles.iconText}>{abbreviate(item.name)}</Text>
      </View>
      <Text style={styles.name}>{item.name}</Text>
      <View style={[styles.indicator, { backgroundColor: sc.bg }]}>
        <Text style={[styles.indicatorText, { color: sc.text }]}>
          {statusLabel}
        </Text>
      </View>
      {item.requests > 0 && (
        <Text style={styles.meta}>
          {item.requests} 请求 · {formatLatency(item.avg_latency)}
        </Text>
      )}
    </View>
  )
}

export default function ProvidersScreen() {
  const providers = useStore((s) => s.providers)
  const providersLoading = useStore((s) => s.providersLoading)
  const providersError = useStore((s) => s.providersError)
  const fetchProviders = useStore((s) => s.fetchProviders)

  usePolling(fetchProviders, 15000)

  const data = useMemo(
    () => providers?.providers ?? [],
    [providers?.providers],
  )

  // 将数据配对成行（每行2个）
  const rows = useMemo(() => {
    const result: ProviderInfo[][] = []
    for (let i = 0; i < data.length; i += 2) {
      result.push(data.slice(i, i + 2))
    }
    return result
  }, [data])

  return (
    <PageContainer
      loading={providersLoading && !providers}
      error={providersError}
    >
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scrollContent}
      >
        <View style={styles.headerWrap}>
          <PageHeader
            title="提供商"
            subtitle={`共 ${providers?.total ?? 0} 个提供商`}
          />
        </View>

        {data.length === 0 && !providersLoading ? (
          <View style={styles.empty}>
            <Text style={styles.emptyIcon}>🔌</Text>
            <Text style={styles.emptyText}>暂无提供商数据</Text>
          </View>
        ) : (
          <View style={styles.grid}>
            {rows.map((row, rowIndex) => (
              <View key={rowIndex} style={styles.row}>
                {row.map((item) => (
                  <ProviderCard key={item.name} item={item} />
                ))}
                {/* 如果这一行只有1个，补一个空位 */}
                {row.length === 1 && <View style={styles.card} />}
              </View>
            ))}
          </View>
        )}
      </ScrollView>
    </PageContainer>
  )
}

const styles = StyleSheet.create({
  scrollContent: {
    paddingBottom: Spacing[16],
  },
  headerWrap: {
    paddingHorizontal: Spacing[4],
    paddingTop: Spacing[4],
  },
  grid: {
    paddingHorizontal: Spacing[4],
    gap: Spacing[4],
  },
  row: {
    flexDirection: 'row',
    gap: Spacing[4],
  },
  card: {
    flex: 1,
    backgroundColor: Colors.card,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.border,
    padding: Spacing[4],
    alignItems: 'center',
    ...Shadow.card,
  },
  icon: {
    width: 44,
    height: 44,
    borderRadius: Radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: Spacing[3],
  },
  iconText: {
    fontSize: Typography.fontSize.sm,
    fontWeight: Typography.fontWeight.bold,
    color: '#fff',
    fontFamily: Typography.fontFamily.body,
  },
  name: {
    fontSize: Typography.fontSize.base,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    marginBottom: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
  indicator: {
    paddingHorizontal: Spacing[3],
    paddingVertical: 2,
    borderRadius: Radius.full,
  },
  indicatorText: {
    fontSize: Typography.fontSize.xs,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
  meta: {
    fontSize: Typography.fontSize.xs,
    color: Colors.textMuted,
    marginTop: Spacing[1],
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
