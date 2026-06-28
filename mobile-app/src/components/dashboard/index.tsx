import { memo } from 'react'
import { View, Text, StyleSheet, TouchableOpacity } from 'react-native'
import { Colors, Typography, Spacing, Radius, Shadow } from '../../theme'
import { getProviderColor, getStatusColor } from '../../theme'
import { formatLatency, formatNumber } from '../../utils'
import type { ProviderInfo, ModelInfo } from '../../api'

interface ProviderChipProps {
  item: ProviderInfo
  onPress: () => void
}

function ProviderChipComponent({ item, onPress }: ProviderChipProps) {
  const sc = getStatusColor(item.status)
  return (
    <TouchableOpacity
      style={styles.chip}
      onPress={onPress}
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
}

interface ActiveModelItemProps {
  item: ModelInfo
  onPress: () => void
}

function ActiveModelItemComponent({ item, onPress }: ActiveModelItemProps) {
  const color = getProviderColor(item.provider || item.model)
  const sc = getStatusColor(item.status)
  return (
    <TouchableOpacity
      style={styles.modelItem}
      onPress={onPress}
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
      <Text style={styles.modelLatency}>{formatLatency(item.avg_latency)}</Text>
    </TouchableOpacity>
  )
}

interface ModelRankRowProps {
  item: ModelInfo
  maxRequests: number
}

function ModelRankRowComponent({ item, maxRequests }: ModelRankRowProps) {
  const color = getProviderColor(item.provider || item.model)
  return (
    <View style={styles.tableRow}>
      <View style={[styles.td, { flex: 2 }]}>
        <View style={[styles.tableDot, { backgroundColor: color }]} />
        <Text style={styles.tableModel} numberOfLines={1}>
          {item.model}
        </Text>
      </View>
      <Text style={[styles.tdValue, { flex: 1 }]}>
        {formatNumber(item.requests)}
      </Text>
      <Text style={[styles.tdValue, { flex: 1 }]}>
        {formatNumber(item.total_tokens)}
      </Text>
    </View>
  )
}

export const ProviderChip = memo(ProviderChipComponent)
export const ActiveModelItem = memo(ActiveModelItemComponent)
export const ModelRankRow = memo(ModelRankRowComponent)

const styles = StyleSheet.create({
  // Provider chip
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
  // Model list item
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
  // Table row
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
