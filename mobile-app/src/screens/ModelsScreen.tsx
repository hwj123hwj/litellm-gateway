import { useCallback, useMemo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { FlashList } from '@shopify/flash-list'
import { useStore } from '../store'
import { usePolling } from '../hooks'
import { PageContainer, PageHeader, ItemSeparator } from '../components'
import { ModelCard } from '../components/models'
import { Colors, Typography, Spacing } from '../theme'
import type { ModelInfo } from '../api'

export default function ModelsScreen() {
  const models = useStore((s) => s.models)
  const modelsLoading = useStore((s) => s.modelsLoading)
  const modelsError = useStore((s) => s.modelsError)
  const fetchModels = useStore((s) => s.fetchModels)

  usePolling(fetchModels, 15000)

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
    <PageContainer loading={modelsLoading && !models} error={modelsError} onRetry={fetchModels}>
      <View style={styles.container}>
        <PageHeader
          title="模型管理"
          subtitle={`共 ${models?.total ?? 0} 个模型，${activeCount} 个活跃`}
        />
      </View>

      {sortedModels.length === 0 && !modelsLoading ? (
        <View style={styles.empty}>
          <Text style={styles.emptyIcon}>📭</Text>
          <Text style={styles.emptyText}>暂无模型数据</Text>
        </View>
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
