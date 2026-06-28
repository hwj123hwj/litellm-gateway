import { Platform } from 'react-native'

/**
 * 字体配置
 * Web 版使用 Google Fonts (Varela Round + Nunito Sans)
 * RN 端使用系统字体回退
 */
export const Typography = {
  fontFamily: {
    heading: Platform.select({
      android: 'sans-serif-medium',
      ios: 'System',
      default: 'System',
    }),
    body: Platform.select({
      android: 'sans-serif',
      ios: 'System',
      default: 'System',
    }),
  },

  fontSize: {
    xs: 11,
    sm: 12,
    base: 13,
    md: 14,
    lg: 15,
    xl: 16,
    '2xl': 18,
    '3xl': 20,
    '4xl': 24,
    '5xl': 28,
  },

  fontWeight: {
    normal: '400' as const,
    medium: '500' as const,
    semibold: '600' as const,
    bold: '700' as const,
  },

  lineHeight: {
    tight: 1.2,
    normal: 1.4,
    relaxed: 1.6,
  },
}
