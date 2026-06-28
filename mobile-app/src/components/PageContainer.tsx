import { View, Text, StyleSheet, ActivityIndicator, TouchableOpacity } from 'react-native'
import { Colors, Typography, Spacing, Radius } from '../theme'

interface Props {
  loading?: boolean
  error?: string | null
  /** error 状态下点击重试的回调；传入后会在错误 banner 上显示重试按钮 */
  onRetry?: () => void
  children?: React.ReactNode
}

export function PageContainer({ loading, error, onRetry, children }: Props) {
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
      <View style={styles.errorWrap}>
        <View style={styles.errorBanner}>
          <Text style={styles.errorIcon}>⚠</Text>
          <Text style={styles.errorText}>{error}</Text>
        </View>
        {onRetry && (
          <TouchableOpacity
            style={styles.retryBtn}
            onPress={onRetry}
            activeOpacity={0.7}
          >
            <Text style={styles.retryBtnText}>重试</Text>
          </TouchableOpacity>
        )}
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
  errorWrap: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: Spacing[4],
    gap: Spacing[4],
  },
  errorBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing[2],
    padding: Spacing[4],
    backgroundColor: Colors.redLight,
    borderRadius: Radius.lg,
  },
  errorIcon: {
    fontSize: 20,
  },
  errorText: {
    flex: 1,
    fontSize: Typography.fontSize.md,
    color: Colors.red,
    fontFamily: Typography.fontFamily.body,
  },
  retryBtn: {
    paddingVertical: Spacing[2],
    paddingHorizontal: Spacing[6],
    backgroundColor: Colors.terracotta[700],
    borderRadius: Radius.md,
  },
  retryBtnText: {
    color: '#fff',
    fontSize: Typography.fontSize.md,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
})
