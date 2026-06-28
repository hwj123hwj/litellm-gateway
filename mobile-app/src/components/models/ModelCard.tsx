import { memo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import {
  Colors,
  Typography,
  Spacing,
  Radius,
  Shadow,
  getProviderColor,
  getStatusColor,
} from '../../theme'
import { formatNumber, formatLatency } from '../../utils'
import type { ModelInfo } from '../../api'

function ModelCardComponent({ item }: { item: ModelInfo }) {
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
}

export const ModelCard = memo(ModelCardComponent)

const styles = StyleSheet.create({
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
})
