import { memo } from 'react'
import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing } from '../theme'

interface EmptyStateProps {
  icon: string
  message: string
}

function EmptyStateComponent({ icon, message }: EmptyStateProps) {
  return (
    <View style={styles.empty}>
      <Text style={styles.emptyIcon}>{icon}</Text>
      <Text style={styles.emptyText}>{message}</Text>
    </View>
  )
}

export const EmptyState = memo(EmptyStateComponent)

const styles = StyleSheet.create({
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
