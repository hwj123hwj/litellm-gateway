import { createBottomTabNavigator } from '@react-navigation/bottom-tabs'
import { Text, StyleSheet, Platform } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
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
  // 获取底部安全区域高度（Android 手势导航栏 / iOS Home Indicator）
  // 确保底部 Tab 按钮不被系统导航栏遮挡
  const insets = useSafeAreaInsets()
  const bottomInset = Math.max(insets.bottom, 0)

  // 基础高度 + 底部安全区域
  const baseHeight = Platform.OS === 'ios' ? 50 : 56
  const tabBarHeight = baseHeight + bottomInset

  return (
    <Tab.Navigator
      screenOptions={({ route }: { route: { name: string } }) => ({
        headerShown: false,
        tabBarIcon: ({ focused }: { focused: boolean }) => (
          <Text style={[styles.icon, focused && styles.iconActive]}>
            {TAB_ICONS[route.name] ?? '📄'}
          </Text>
        ),
        tabBarLabel: TAB_LABELS[route.name] ?? route.name,
        tabBarActiveTintColor: Colors.terracotta[700],
        tabBarInactiveTintColor: Colors.textMuted,
        tabBarStyle: {
          ...styles.tabBar,
          height: tabBarHeight,
          paddingBottom: bottomInset + 6,
        },
        tabBarLabelStyle: styles.tabLabel,
        tabBarItemStyle: {
          paddingVertical: 4,
        },
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
