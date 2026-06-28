import { View, Text, StyleSheet, ActivityIndicator } from 'react-native'
import { Colors, Typography, Spacing, Radius } from '../theme'

interface Props {
  loading?: boolean
  error?: string | null
  children?: React.ReactNode
}

export function PageContainer({ loading, error, children }: Props) {
  if (loading) {
    return (
      <View style={styles.center}>
        <ActivityIndicator size="small" color={Colors.terracotta[600]} />
        <Text style={styles.loadingText}>加载中...</Text>
      </View>
    )
  }

  if (error) {
    return (
      <View style={styles.errorBanner}>
        <Text style={styles.errorIcon}>⚠</Text>
        <Text style={styles.errorText}>{error}</Text>
      </View>
    )
  }

  return <View style={styles.container}>{children}</View>
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    gap: Spacing[4],
  },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: Spacing[2],
  },
  loadingText: {
    fontSize: Typography.fontSize.md,
    color: Colors.textMuted,
    fontFamily: Typography.fontFamily.body,
  },
  errorBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
    padding: Spacing[3],
    margin: Spacing[4],
    backgroundColor: Colors.redLight,
    borderRadius: Radius.sm,
  },
  errorIcon: {
    fontSize: 16,
  },
  errorText: {
    flex: 1,
    fontSize: Typography.fontSize.base,
    color: Colors.red,
    fontFamily: Typography.fontFamily.body,
  },
})
