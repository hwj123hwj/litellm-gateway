import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing, Radius, Shadow } from '../theme'

interface Props {
  label: string
  value: string
  trend?: string
  trendType?: 'up' | 'down' | 'neutral'
}

export function KpiCard({ label, value, trend, trendType }: Props) {
  return (
    <View style={styles.card}>
      <Text style={styles.label}>{label}</Text>
      <Text style={styles.value}>{value}</Text>
      {trend && (
        <Text
          style={[
            styles.trend,
            trendType === 'up' && styles.trendUp,
            trendType === 'down' && styles.trendDown,
            trendType === 'neutral' && styles.trendNeutral,
          ]}
        >
          {trend}
        </Text>
      )}
    </View>
  )
}

const styles = StyleSheet.create({
  card: {
    flex: 1,
    backgroundColor: Colors.card,
    borderRadius: Radius.lg,
    padding: Spacing[4],
    borderWidth: 1,
    borderColor: Colors.border,
    ...Shadow.card,
  },
  label: {
    fontSize: Typography.fontSize.sm,
    color: Colors.textMuted,
    fontWeight: Typography.fontWeight.medium,
    marginBottom: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
  value: {
    fontSize: Typography.fontSize['3xl'],
    color: Colors.text,
    fontWeight: Typography.fontWeight.bold,
    fontFamily: Typography.fontFamily.heading,
    lineHeight: Typography.fontSize['3xl'] * Typography.lineHeight.tight,
  },
  trend: {
    fontSize: Typography.fontSize.sm,
    fontWeight: Typography.fontWeight.semibold,
    marginTop: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
  trendUp: { color: Colors.green },
  trendDown: { color: Colors.red },
  trendNeutral: { color: Colors.amber },
})
