import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing } from '../theme'

interface Props {
  title: string
  subtitle?: string
}

export function PageHeader({ title, subtitle }: Props) {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>{title}</Text>
      {subtitle && <Text style={styles.subtitle}>{subtitle}</Text>}
    </View>
  )
}

const styles = StyleSheet.create({
  container: {
    marginBottom: Spacing[1],
  },
  title: {
    fontSize: Typography.fontSize['4xl'],
    color: Colors.terracotta[700],
    fontWeight: Typography.fontWeight.bold,
    fontFamily: Typography.fontFamily.heading,
  },
  subtitle: {
    fontSize: Typography.fontSize.md,
    color: Colors.textMuted,
    marginTop: Spacing[1],
    fontFamily: Typography.fontFamily.body,
  },
})
