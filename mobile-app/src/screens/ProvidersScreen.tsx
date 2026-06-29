import { useCallback, useMemo } from 'react'
import { View, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import type { BottomTabNavigationProp } from '@react-navigation/bottom-tabs'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import {
  PageContainer,
  PageHeader,
  EmptyState,
} from '../components'
import { ProviderCard } from '../components/providers'
import { Spacing } from '../theme'
import type { ProviderInfo } from '../api'

type ProvidersNavigation = BottomTabNavigationProp<
  Record<string, object | undefined>,
  'ProvidersTab'
>

const POLL_INTERVAL_MS = 15_000

export default function ProvidersScreen({
  navigation,
}: {
  navigation: ProvidersNavigation
}) {
  const providers = useStore((s) => s.providers)
  const providersLoading = useStore((s) => s.providersLoading)
  const providersError = useStore((s) => s.providersError)
  const fetchProviders = useStore((s) => s.fetchProviders)

  usePolling(fetchProviders, POLL_INTERVAL_MS)

  const goToSettings = useCallback(
    () => navigation.navigate('SettingsTab'),
    [navigation],
  )

  const data = useMemo(
    () => providers?.providers ?? [],
    [providers?.providers],
  )

  const renderItem = useCallback(
    ({ item }: { item: ProviderInfo }) => <ProviderCard item={item} />,
    [],
  )
  const keyExtractor = useCallback((item: ProviderInfo) => item.name, [])

  return (
    <PageContainer
      loading={providersLoading && !providers}
      error={providersError}
      onRetry={providersError?.code === 'AUTH' ? goToSettings : fetchProviders}
    >
      <View style={styles.headerWrap}>
        <PageHeader
          title="提供商"
          subtitle={`共 ${providers?.total ?? 0} 个提供商`}
        />
      </View>

      {data.length === 0 && !providersLoading ? (
        <EmptyState icon="🔌" message="暂无提供商数据" />
      ) : (
        <FlashList
          data={data}
          renderItem={renderItem}
          keyExtractor={keyExtractor}
          numColumns={2}
          contentContainerStyle={styles.listContent}
          // numColumns 模式下禁用 ItemSeparator，改用 item 内部 margin 控制间距，
          // 否则 separator 会出现在水平相邻的两列之间产生错位。
          // 卡片间距由 ProviderCard 的 margin + listContent 控制。
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
