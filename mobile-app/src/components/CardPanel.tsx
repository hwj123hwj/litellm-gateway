import { View, Text, StyleSheet } from 'react-native'
import { Colors, Typography, Spacing, Radius, Shadow } from '../theme'

interface Props {
  title: string
  action?: string
  onAction?: () => void
  children: React.ReactNode
}

export function CardPanel({ title, action, onAction, children }: Props) {
  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>{title}</Text>
        {action && onAction && (
          <Text style={styles.action} onPress={onAction}>
            {action}
          </Text>
        )}
      </View>
      {children}
    </View>
  )
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.card,
    borderRadius: Radius.lg,
    borderWidth: 1,
    borderColor: Colors.border,
    padding: Spacing[4],
    ...Shadow.card,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: Spacing[4],
  },
  title: {
    fontSize: Typography.fontSize.xl,
    color: Colors.text,
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.heading,
  },
  action: {
    fontSize: Typography.fontSize.base,
    color: Colors.terracotta[600],
    fontWeight: Typography.fontWeight.semibold,
    fontFamily: Typography.fontFamily.body,
  },
})
