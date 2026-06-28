import { useCallback, useMemo } from 'react'
import { View, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import {
  PageContainer,
  PageHeader,
  ItemSeparator,
  EmptyState,
} from '../components'
import { ModelCard } from '../components/models'
import { Spacing } from '../theme'
import type { ModelInfo } from '../api'

const POLL_INTERVAL_MS = 15_000

export default function ModelsScreen() {
  const models = useStore((s) => s.models)
  const modelsLoading = useStore((s) => s.modelsLoading)
  const modelsError = useStore((s) => s.modelsError)
  const fetchModels = useStore((s) => s.fetchModels)

  usePolling(fetchModels, POLL_INTERVAL_MS)

  const sortedModels = useMemo(
    () =>
      [...(models?.models ?? [])].sort((a, b) => b.requests - a.requests),
    [models?.models],
  )

  const activeCount = useMemo(
    () => sortedModels.filter((m) => m.status !== 'idle').length,
    [sortedModels],
  )

  const renderItem = useCallback(
    ({ item }: { item: ModelInfo }) => <ModelCard item={item} />,
    [],
  )
  const keyExtractor = useCallback((item: ModelInfo) => item.model, [])

  return (
    <PageContainer
      loading={modelsLoading && !models}
      error={modelsError}
      onRetry={fetchModels}
    >
      <View style={styles.container}>
        <PageHeader
          title="模型管理"
          subtitle={`共 ${models?.total ?? 0} 个模型，${activeCount} 个活跃`}
        />
      </View>

      {sortedModels.length === 0 && !modelsLoading ? (
        <EmptyState icon="📭" message="暂无模型数据" />
      ) : (
        <FlashList
          data={sortedModels}
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
  container: {
    paddingHorizontal: Spacing[4],
    paddingTop: Spacing[4],
  },
  listContent: {
    paddingHorizontal: Spacing[4],
    paddingBottom: Spacing[16],
  },
})
