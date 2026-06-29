import { useCallback, useMemo } from 'react'
import { View, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import type { BottomTabNavigationProp } from '@react-navigation/bottom-tabs'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import {
  PageContainer,
  PageHeader,
  ItemSeparator,
  EmptyState,
} from '../components'
import { LogEntryItem } from '../components/logs'
import { Spacing } from '../theme'
import type { LogEntry } from '../api'

type LogsNavigation = BottomTabNavigationProp<
  Record<string, object | undefined>,
  'LogsTab'
>

/** 常量化魔法数字，便于全局调整 */
const LOG_FETCH_LIMIT = 100
const POLL_INTERVAL_MS = 10_000

export default function LogsScreen({
  navigation,
}: {
  navigation: LogsNavigation
}) {
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

  const fetch = useCallback(() => fetchLogs(LOG_FETCH_LIMIT), [fetchLogs])
  usePolling(fetch, POLL_INTERVAL_MS)

  const goToSettings = useCallback(
    () => navigation.navigate('SettingsTab'),
    [navigation],
  )

  const data = useMemo(() => logs?.logs ?? [], [logs?.logs])

  const renderItem = useCallback(
    ({ item }: { item: LogEntry }) => <LogEntryItem item={item} />,
    [],
  )

  return (
    <PageContainer
      loading={logsLoading && !logs}
      error={logsError}
      onRetry={logsError?.code === 'AUTH' ? goToSettings : fetch}
    >
      <View style={styles.headerWrap}>
        <PageHeader
          title="活动日志"
          subtitle={`最近 ${logs?.total ?? 0} 条请求记录`}
        />
      </View>

      {data.length === 0 && !logsLoading ? (
        <EmptyState icon="📄" message="暂无日志，等待第一个请求..." />
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
})
