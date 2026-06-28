import { memo } from 'react'
import { View, StyleSheet } from 'react-native'
import { Spacing } from '../theme'

/**
 * 列表项之间的默认分隔线。
 * 提取为模块级、被 React.memo 包裹的稳定组件，
 * 避免每次 render 都内联创建新箭头函数（FlashList / FlatList 反模式）。
 */
function ItemSeparatorComponent() {
  return <View style={styles.separator} />
}

export const ItemSeparator = memo(ItemSeparatorComponent)

const styles = StyleSheet.create({
  separator: {
    height: Spacing[3],
  },
})
