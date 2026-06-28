import { useEffect } from 'react'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'

const COLORS: Record<string, string> = {
  glm: '#9a3412', mimo: '#059669', longcat: '#6366f1',
  easyclaw: '#dc2626', deepv: '#2563eb', copilot: '#7c3aed', chatgpt: '#10b981',
}

function color(name: string) {
  const k = name.toLowerCase()
  for (const [key, val] of Object.entries(COLORS)) {
    if (k.includes(key)) return val
  }
  return '#78716c'
}

function abbr(name: string) {
  return name.split(/[-_]/).map((w) => w[0]).join('').toUpperCase().slice(0, 3)
}

export default function Providers() {
  const { providers, providersLoading, providersError, fetchProviders } = useStore()

  useEffect(() => {
    fetchProviders()
    const timer = setInterval(fetchProviders, 15000)
    return () => clearInterval(timer)
  }, [fetchProviders])

  if (providersLoading && !providers) return <div className="loading">加载中...</div>
  if (providersError) return <div className="error-banner">⚠ {providersError}</div>
  if (!providers) return null

  return (
    <>
      <PageHeader title="提供商" subtitle={`共 ${providers.total} 个提供商`} />
      <div className="provider-grid">
        {providers.providers.map((p) => (
          <div key={p.name} className="provider-card">
            <div className="provider-icon" style={{ background: `linear-gradient(135deg, ${color(p.name)}, ${color(p.name)}cc)` }}>
              {abbr(p.name)}
            </div>
            <div className="provider-name">{p.name}</div>
            <span className={`provider-indicator ${p.status}`}>
              {p.status === 'online' ? '● 在线' : p.status === 'degraded' ? '● 降级' : p.status === 'offline' ? '● 离线' : '● 未知'}
            </span>
            {p.requests > 0 && (
              <div style={{ fontSize: 11, color: '#a8a29e', marginTop: 6 }}>
                {p.requests} 请求 · {p.avg_latency < 1000 ? p.avg_latency.toFixed(0) + 'ms' : (p.avg_latency / 1000).toFixed(1) + 's'}
              </div>
            )}
          </div>
        ))}
        {providers.providers.length === 0 && (
          <div className="empty-state" style={{ gridColumn: '1 / -1' }}>
            <div className="empty-icon">🔌</div>
            <div className="empty-text">暂无提供商数据</div>
          </div>
        )}
      </div>
    </>
  )
}
