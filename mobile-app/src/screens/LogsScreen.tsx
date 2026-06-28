import { useEffect, useCallback, useMemo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { PageContainer, PageHeader } from '../components'
import { Colors, Typography, Spacing, Radius, Shadow } from '../theme'
import type { LogEntry } from '../api'

function formatTime(ts: string): string {
  const d = new Date(ts)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return Math.floor(diff / 60_000) + ' 分钟前'
  if (diff < 86_400_000) return Math.floor(diff / 3_600_000) + ' 小时前'
  return d.toLocaleDateString('zh-CN')
}

function formatLatency(ms: number): string {
  return ms < 1000 ? ms.toFixed(0) + 'ms' : (ms / 1000).toFixed(1) + 's'
}

export default function LogsScreen() {
  const { logs, logsLoading, logsError, fetchLogs } = useStore()

  useEffect(() => {
    fetchLogs(100)
    const timer = setInterval(() => fetchLogs(100), 10000)
    return () => clearInterval(timer)
  }, [fetchLogs])

  const data = useMemo(() => logs?.logs ?? [], [logs?.logs])

  const renderItem = useCallback(({ item, index }: { item: LogEntry; index: number }) => {
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
        <Text style={styles.time}>{formatTime(item.timestamp)}</Text>
      </View>
    )
  }, [])

  return (
    <PageContainer loading={logsLoading && !logs} error={logsError}>
      <View style={styles.headerWrap}>
        <PageHeader
          title="活动日志"
          subtitle={`最近 ${logs?.total ?? 0} 条请求记录`}
        />
      </View>

      {data.length === 0 && !logsLoading ? (
        <View style={styles.empty}>
          <Text style={styles.emptyIcon}>📄</Text>
          <Text style={styles.emptyText}>暂无日志，等待第一个请求...</Text>
        </View>
      ) : (
        <FlashList
          data={data}
          renderItem={renderItem}
          keyExtractor={(_, i) => String(i)}
          contentContainerStyle={styles.listContent}
          ItemSeparatorComponent={() => <View style={styles.separator} />}
        />
      )}
    </PageContainer>
  )
}

const styles = StyleSheet.create({
  headerWrap: {
    paddingHorizontal: Spacing[4],
    paddingTop: Spacing[4],
  },
  listContent: {
    paddingHorizontal: Spacing[4],
    paddingBottom: Spacing[16],
  },
  separator: {
    height: Spacing[3],
  },
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
