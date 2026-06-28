/**
 * 间距与圆角系统
 * 基于 4px 网格
 */
export const Spacing = {
  1: 4,
  2: 8,
  3: 12,
  4: 16,
  5: 20,
  6: 24,
  8: 32,
  10: 40,
  12: 48,
  16: 64,
}

export const Radius = {
  sm: 10,
  md: 16,
  lg: 20,
  xl: 24,
  full: 999,
}

export const Shadow = {
  card: {
    shadowColor: 'rgba(154, 52, 18, 0.08)',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 1,
    shadowRadius: 24,
    elevation: 3,
  },
  cardHover: {
    shadowColor: 'rgba(154, 52, 18, 0.12)',
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 1,
    shadowRadius: 32,
    elevation: 6,
  },
}
