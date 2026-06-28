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
import { formatLatency, abbreviate } from '../../utils'
import type { ProviderInfo } from '../../api'

function ProviderCardComponent({ item }: { item: ProviderInfo }) {
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

export const ProviderCard = memo(ProviderCardComponent)

const styles = StyleSheet.create({
  card: {
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
})
