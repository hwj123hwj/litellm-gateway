import { memo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing, Radius, Shadow } from '../../theme'
import { formatRelativeTime, formatLatency } from '../../utils'
import type { LogEntry } from '../../api'

function LogEntryItemComponent({ item }: { item: LogEntry }) {
  const isSuccess = item.status_code >= 200 && item.status_code < 400
  const hasError = !!item.error
  const statusClass = hasError ? 'error' : isSuccess ? 'success' : 'error'
  const statusText = hasError
    ? '错误'
    : isSuccess
      ? '成功'
      : `${item.status_code}`

  const statusBgColor =
    statusClass === 'success' ? Colors.greenLight : Colors.redLight
  const statusTextColor =
    statusClass === 'success' ? Colors.green : Colors.red
  const dotColor = statusClass === 'success' ? Colors.green : Colors.red

  const detailParts = [`${item.method} ${item.path}`]
  if (item.latency_ms > 0) detailParts.push(formatLatency(item.latency_ms))
  if (item.input_tokens > 0)
    detailParts.push(`${item.input_tokens + item.output_tokens} tokens`)
  if (item.is_stream) detailParts.push('流式')
  if (item.error) detailParts.push(item.error)

  return (
    <View style={styles.entry}>
      <View style={styles.entryTop}>
        <View style={styles.entryTopLeft}>
          <View style={[styles.dot, { backgroundColor: dotColor }]} />
          <Text style={styles.model} numberOfLines={1}>
            {item.provider ? `${item.provider} · ` : ''}
            {item.model || item.path}
          </Text>
        </View>
        <View style={[styles.statusBadge, { backgroundColor: statusBgColor }]}>
          <Text style={[styles.statusText, { color: statusTextColor }]}>
            {statusText}
          </Text>
        </View>
      </View>
      <Text style={styles.detail} numberOfLines={2}>
        {detailParts.join(' · ')}
      </Text>
      <Text style={styles.time}>{formatRelativeTime(item.timestamp)}</Text>
    </View>
  )
}

export const LogEntryItem = memo(LogEntryItemComponent)

const styles = StyleSheet.create({
  entry: {
    backgroundColor: Colors.card,
    borderRadius: Radius.lg,
    borderWidth: 1,
    borderColor: Colors.border,
    padding: Spacing[4],
    ...Shadow.card,
  },
  entryTop: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: Spacing[1],
  },
  entryTopLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
    flex: 1,
    marginRight: Spacing[2],
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    flexShrink: 0,
  },
  model: {
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.semibold,
    color: Colors.text,
    flex: 1,
    fontFamily: Typography.fontFamily.body,
  },
  statusBadge: {
    paddingHorizontal: Spacing[3],
    paddingVertical: 2,
    borderRadius: Radius.full,
    flexShrink: 0,
  },
  statusText: {
    fontSize: Typography.fontSize.xs,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
  detail: {
    fontSize: Typography.fontSize.base,
    color: Colors.textSecondary,
    lineHeight: Typography.fontSize.base * Typography.lineHeight.normal,
    fontFamily: Typography.fontFamily.body,
  },
  time: {
    fontSize: Typography.fontSize.xs,
    color: Colors.textMuted,
    marginTop: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
})
