/**
 * 共享格式化工具函数
 * 从 DashboardScreen / ModelsScreen / LogsScreen 中提取，消除重复定义
 */

/** 格式化大数字: 1234 -> "1.2K", 1500000 -> "1.5M" */
export function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}

/** 格式化延迟: 800 -> "800ms", 2500 -> "2.5s" */
export function formatLatency(ms: number): string {
  return ms < 1000 ? ms.toFixed(0) + 'ms' : (ms / 1000).toFixed(1) + 's'
}

/** 格式化时间为相对时间: 刚刚 / N 分钟前 / N 小时前 / 日期 */
export function formatRelativeTime(ts: string): string {
  const d = new Date(ts)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return Math.floor(diff / 60_000) + ' 分钟前'
  if (diff < 86_400_000) return Math.floor(diff / 3_600_000) + ' 小时前'
  return d.toLocaleDateString('zh-CN')
}

/** 从 provider 名提取缩写（最多3个大写字母） */
export function abbreviate(name: string): string {
  return name
    .split(/[-_]/)
    .map((w) => w[0])
    .join('')
    .toUpperCase()
    .slice(0, 3)
}
