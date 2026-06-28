import { useCallback, useMemo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import { PageContainer, PageHeader, ItemSeparator } from '../components'
import { LogEntryItem } from '../components/logs'
import { Colors, Typography, Spacing } from '../theme'
import type { LogEntry } from '../api'

export default function LogsScreen() {
  const logs = useStore((s) => s.logs)
  const logsLoading = useStore((s) => s.logsLoading)
  const logsError = useStore((s) => s.logsError)
  const fetchLogs = useStore((s) => s.fetchLogs)

  // 稳定的 keyExtractor：timestamp + model + path 组合，
  // 并在末尾附加 index 作为兜底，防止极端情况下两条日志字段完全相同时 key 碰撞。
  const keyExtractor = useCallback(
    (item: LogEntry, index: number): string => {
      return `${item.timestamp}|${item.model}|${item.path}|${index}`
    },
    [],
  )

  usePolling(() => fetchLogs(100), 10000)

  const data = useMemo(() => logs?.logs ?? [], [logs?.logs])

  const renderItem = useCallback(
    ({ item }: { item: LogEntry }) => <LogEntryItem item={item} />,
    [],
  )

  return (
    <PageContainer loading={logsLoading && !logs} error={logsError} onRetry={() => fetchLogs(100)}>
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
          keyExtractor={keyExtractor}
          contentContainerStyle={styles.listContent}
          ItemSeparatorComponent={ItemSeparator}
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
