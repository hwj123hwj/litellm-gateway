import { View, Text, StyleSheet, ActivityIndicator, TouchableOpacity } from 'react-native'
import { Colors, Typography, Spacing, Radius } from '../theme'
import type { ApiError } from '../api'

interface Props {
  loading?: boolean
  error?: ApiError | null
  /**
   * error 状态下点击重试的回调；传入后会在错误 banner 上显示重试按钮。
   * AUTH 类错误（401/403）时优先显示此回调（用于跳转到设置页配置 Key）。
   */
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
    // 根据错误 code 差异化展示 CTA
    const isAuthError = error.code === 'AUTH'
    const ctaLabel = isAuthError ? '去配置 Key' : '重试'

    return (
      <View style={styles.errorWrap}>
        <View style={styles.errorBanner}>
          <Text style={styles.errorIcon}>{isAuthError ? '🔑' : '⚠'}</Text>
          <Text style={styles.errorText}>{error.message}</Text>
        </View>
        {onRetry && (
          <TouchableOpacity
            style={styles.retryBtn}
            onPress={onRetry}
            activeOpacity={0.7}
          >
            <Text style={styles.retryBtnText}>{ctaLabel}</Text>
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
