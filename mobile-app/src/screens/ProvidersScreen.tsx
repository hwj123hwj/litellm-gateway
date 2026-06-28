import { useCallback, useMemo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import { PageContainer, PageHeader, ItemSeparator } from '../components'
import { ProviderCard } from '../components/providers'
import { Colors, Typography, Spacing } from '../theme'
import type { ProviderInfo } from '../api'

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

  const renderItem = useCallback(
    ({ item }: { item: ProviderInfo }) => <ProviderCard item={item} />,
    [],
  )
  const keyExtractor = useCallback((item: ProviderInfo) => item.name, [])

  return (
    <PageContainer
      loading={providersLoading && !providers}
      error={providersError}
      onRetry={fetchProviders}
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
        <FlashList
          data={data}
          renderItem={renderItem}
          keyExtractor={keyExtractor}
          numColumns={2}
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
