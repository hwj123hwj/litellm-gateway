/**
 * Litellm Admin 主题色系
 * 从 Web 版 CSS 变量迁移，适配 React Native StyleSheet
 */

export const Colors = {
  // 主色 - 赤陶色系
  terracotta: {
    900: '#7a2b0b',
    700: '#9a3412',
    600: '#c2410c',
    500: '#d97706',
    100: '#fef3c7',
    50: '#fffbeb',
  },

  // 背景色
  cream: '#fef9f0',
  warmWhite: '#fffcf7',
  card: '#ffffff',
  background: '#f5ece4',

  // 边框
  border: '#ede0d8',
  borderLight: '#f5eee9',

  // 文字
  text: '#1c1917',
  textSecondary: '#78716c',
  textMuted: '#a8a29e',

  // 状态色
  green: '#059669',
  greenLight: '#d1fae5',
  red: '#dc2626',
  redLight: '#fee2e2',
  amber: '#d97706',
  amberLight: '#fef3c7',
  blue: '#2563eb',
  blueLight: '#dbeafe',

  // Provider 特定颜色
  providerColors: {
    glm: '#9a3412',
    mimo: '#059669',
    longcat: '#6366f1',
    easyclaw: '#dc2626',
    deepv: '#2563eb',
    copilot: '#7c3aed',
    chatgpt: '#10b981',
  } as Record<string, string>,
}

export function getProviderColor(name: string): string {
  const key = name.toLowerCase()
  for (const [k, v] of Object.entries(Colors.providerColors)) {
    if (key.includes(k)) return v
  }
  return Colors.textSecondary
}

export function getStatusColor(status: string): {
  bg: string
  text: string
} {
  switch (status) {
    case 'online':
      return { bg: Colors.greenLight, text: Colors.green }
    case 'degraded':
      return { bg: Colors.amberLight, text: Colors.amber }
    case 'offline':
      return { bg: Colors.redLight, text: Colors.red }
    default:
      return { bg: '#f3f4f6', text: Colors.textMuted }
  }
}
