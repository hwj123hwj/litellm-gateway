import { useEffect } from 'react'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'

const COLORS: Record<string, string> = {
  glm: '#9a3412', mimo: '#059669', longcat: '#6366f1',
  easyclaw: '#dc2626', deepv: '#2563eb', copilot: '#7c3aed',
}

function color(name: string) {
  const k = name.toLowerCase()
  for (const [key, val] of Object.entries(COLORS)) {
    if (k.includes(key)) return val
  }
  return '#78716c'
}

function fmt(n: number) {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}

export default function Models() {
  const { models, modelsLoading, modelsError, fetchModels } = useStore()

  useEffect(() => {
    fetchModels()
    const timer = setInterval(fetchModels, 15000)
    return () => clearInterval(timer)
  }, [fetchModels])

  if (modelsLoading && !models) return <div className="loading">加载中...</div>
  if (modelsError) return <div className="error-banner">⚠ {modelsError}</div>
  if (!models) return null

  const activeCount = models.models.filter((m) => m.status !== 'idle').length

  return (
    <>
      <PageHeader title="模型管理" subtitle={`共 ${models.total} 个模型，${activeCount} 个活跃`} />
      <div className="model-detail-list">
        {models.models
          .sort((a, b) => b.requests - a.requests)
          .map((m) => (
            <div key={m.model} className="model-detail-card">
              <div className="md-header">
                <div className="md-icon" style={{ background: color(m.provider || m.model) }}>
                  {m.model.slice(0, 2).toUpperCase()}
                </div>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 15 }}>{m.model}</div>
                  <div style={{ fontSize: 12, color: '#a8a29e' }}>{m.provider || '—'}</div>
                </div>
                <span style={{
                  marginLeft: 'auto',
                  fontSize: 11,
                  fontWeight: 600,
                  padding: '3px 10px',
                  borderRadius: 999,
                  background: m.status === 'online' ? '#d1fae5' : m.status === 'degraded' ? '#fef3c7' : '#f3f4f6',
                  color: m.status === 'online' ? '#059669' : m.status === 'degraded' ? '#d97706' : '#a8a29e',
                }}>
                  {m.status === 'online' ? '在线' : m.status === 'degraded' ? '降级' : m.status === 'offline' ? '离线' : '空闲'}
                </span>
              </div>
              <div className="md-stats">
                <div className="md-stat">
                  <div className="md-stat-value">{fmt(m.requests)}</div>
                  <div className="md-stat-label">请求</div>
                </div>
                <div className="md-stat">
                  <div className="md-stat-value">{fmt(m.total_tokens)}</div>
                  <div className="md-stat-label">Tokens</div>
                </div>
                <div className="md-stat">
                  <div className="md-stat-value">
                    {m.avg_latency < 1000 ? m.avg_latency.toFixed(0) + 'ms' : (m.avg_latency / 1000).toFixed(1) + 's'}
                  </div>
                  <div className="md-stat-label">延迟</div>
                </div>
              </div>
            </div>
          ))}
        {models.models.length === 0 && (
          <div className="empty-state">
            <div className="empty-icon">📭</div>
            <div className="empty-text">暂无模型数据</div>
          </div>
        )}
      </div>
    </>
  )
}
