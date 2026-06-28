import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing, Radius } from '../theme'

interface Props {
  status: 'ok' | 'degraded' | 'error' | string
}

export function StatusBadge({ status }: Props) {
  const isOk = status === 'ok'
  const isDegraded = status === 'degraded'

  return (
    <View
      style={[
        styles.badge,
        isOk && styles.badgeOk,
        isDegraded && styles.badgeDegraded,
        !isOk && !isDegraded && styles.badgeError,
      ]}
    >
      <View
        style={[
          styles.dot,
          isOk && styles.dotOk,
          isDegraded && styles.dotDegraded,
          !isOk && !isDegraded && styles.dotError,
        ]}
      />
      <Text
        style={[
          styles.text,
          isOk && styles.textOk,
          isDegraded && styles.textDegraded,
          !isOk && !isDegraded && styles.textError,
        ]}
      >
        {isOk ? '所有系统正常' : isDegraded ? '部分降级' : '未连接'}
      </Text>
    </View>
  )
}

const styles = StyleSheet.create({
  badge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: Spacing[3],
    paddingVertical: 6,
    borderRadius: Radius.full,
  },
  badgeOk: { backgroundColor: Colors.greenLight },
  badgeDegraded: { backgroundColor: Colors.amberLight },
  badgeError: { backgroundColor: Colors.redLight },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  dotOk: { backgroundColor: Colors.green },
  dotDegraded: { backgroundColor: Colors.amber },
  dotError: { backgroundColor: Colors.red },
  text: {
    fontSize: Typography.fontSize.sm,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
  textOk: { color: Colors.green },
  textDegraded: { color: Colors.amber },
  textError: { color: Colors.red },
})
