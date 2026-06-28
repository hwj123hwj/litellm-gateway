import { createBottomTabNavigator } from '@react-navigation/bottom-tabs'
import { Text, StyleSheet } from 'react-native'
import {
  DashboardScreen,
  ModelsScreen,
  ProvidersScreen,
  LogsScreen,
  SettingsScreen,
} from '../screens'
import { Colors, Typography } from '../theme'

const Tab = createBottomTabNavigator()

const TAB_ICONS: Record<string, string> = {
  DashboardTab: '🏠',
  ModelsTab: '📦',
  ProvidersTab: '🔌',
  LogsTab: '📄',
  SettingsTab: '⚙️',
}

const TAB_LABELS: Record<string, string> = {
  DashboardTab: '首页',
  ModelsTab: '模型',
  ProvidersTab: '提供商',
  LogsTab: '日志',
  SettingsTab: '设置',
}

export default function TabNavigator() {
  return (
    <Tab.Navigator
      screenOptions={({ route }) => ({
        headerShown: false,
        tabBarIcon: ({ focused }) => (
          <Text style={[styles.icon, focused && styles.iconActive]}>
            {TAB_ICONS[route.name] ?? '📄'}
          </Text>
        ),
        tabBarLabel: TAB_LABELS[route.name] ?? route.name,
        tabBarActiveTintColor: Colors.terracotta[700],
        tabBarInactiveTintColor: Colors.textMuted,
        tabBarStyle: styles.tabBar,
        tabBarLabelStyle: styles.tabLabel,
      })}
    >
      <Tab.Screen name="DashboardTab" component={DashboardScreen} />
      <Tab.Screen name="ModelsTab" component={ModelsScreen} />
      <Tab.Screen name="ProvidersTab" component={ProvidersScreen} />
      <Tab.Screen name="LogsTab" component={LogsScreen} />
      <Tab.Screen name="SettingsTab" component={SettingsScreen} />
    </Tab.Navigator>
  )
}

const styles = StyleSheet.create({
  tabBar: {
    backgroundColor: Colors.warmWhite,
    borderTopColor: Colors.border,
    borderTopWidth: 1,
    height: 64,
    paddingBottom: 8,
    paddingTop: 4,
  },
  tabLabel: {
    fontSize: Typography.fontSize.xs,
    fontWeight: Typography.fontWeight.medium,
    fontFamily: Typography.fontFamily.body,
  },
  icon: {
    fontSize: 18,
    opacity: 0.5,
  },
  iconActive: {
    opacity: 1,
  },
})
