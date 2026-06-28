import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { getDashboard } from '../api'
import { useFetch } from '../hooks'

const PROVIDER_COLORS: Record<string, string> = {
  glm: '#9a3412',
  mimo: '#059669',
  longcat: '#6366f1',
  easyclaw: '#dc2626',
  deepv: '#2563eb',
  copilot: '#7c3aed',
  chatgpt: '#10b981',
}

function providerColor(name: string): string {
  const key = name.toLowerCase()
  for (const [k, v] of Object.entries(PROVIDER_COLORS)) {
    if (key.includes(k)) return v
  }
  return '#78716c'
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return String(n)
}

export default function Dashboard() {
  const navigate = useNavigate()
  const fetcher = useCallback(() => getDashboard(), [])
  const { data, error, loading } = useFetch(fetcher, { refreshInterval: 10000 })

  if (loading) return <div className="loading">加载中...</div>
  if (error) return <div className="error-banner">⚠ {error}</div>
  if (!data) return null

  const { summary, providers, models } = data

  return (
    <>
      {/* Status row */}
      <div className="status-row">
        <span className="pulse" />
        <span className="status-text">
          {summary.today_requests > 0 ? '所有系统正常运行' : '等待请求...'}
        </span>
        <span className="status-time">运行 {summary.uptime}</span>
      </div>

      {/* KPI */}
      <div className="kpi-grid">
        <div className="kpi-card">
          <div className="kpi-label">📊 今日请求</div>
          <div className="kpi-value">{formatNumber(summary.today_requests)}</div>
        </div>
        <div className="kpi-card">
          <div className="kpi-label">✅ 成功率</div>
          <div className="kpi-value">{summary.success_rate.toFixed(1)}%</div>
          <div className={`kpi-trend ${summary.success_rate >= 99 ? 'up' : summary.success_rate >= 95 ? 'neutral' : 'down'}`}>
            {summary.success_rate >= 99 ? '↑ 优秀' : summary.success_rate >= 95 ? '→ 正常' : '↓ 偏低'}
          </div>
        </div>
        <div className="kpi-card">
          <div className="kpi-label">📦 活跃模型</div>
          <div className="kpi-value">{summary.active_models}</div>
        </div>
        <div className="kpi-card">
          <div className="kpi-label">⏱ 平均延迟</div>
          <div className="kpi-value">{summary.avg_latency_ms < 1000 ? summary.avg_latency_ms.toFixed(0) + 'ms' : (summary.avg_latency_ms / 1000).toFixed(1) + 's'}</div>
          <div className={`kpi-trend ${summary.avg_latency_ms < 2000 ? 'up' : summary.avg_latency_ms < 5000 ? 'neutral' : 'down'}`}>
            {summary.avg_latency_ms < 2000 ? '→ 正常' : '↓ 偏高'}
          </div>
        </div>
      </div>

      {/* Provider strip */}
      {providers.length > 0 && (
        <div className="provider-strip">
          {providers.map((p) => (
            <div
              key={p.name}
              className="provider-chip"
              onClick={() => navigate('/providers')}
            >
              <span className={`p-dot ${p.status}`} />
              <span className="p-label">{p.name}</span>
              <span className={`p-badge ${p.status}`}>
                {p.avg_latency < 1000 ? p.avg_latency.toFixed(0) + 'ms' : (p.avg_latency / 1000).toFixed(1) + 's'}
              </span>
            </div>
          ))}
        </div>
      )}

      {/* Models + Chart */}
      <div className="grid-2col">
        <div className="card-panel">
          <div className="panel-header">
            <div className="panel-title">活跃模型</div>
            <div className="panel-action" onClick={() => navigate('/models')}>查看全部 →</div>
          </div>
          <div className="model-list">
            {models.length === 0 && (
              <div className="empty-state">
                <div className="empty-icon">📭</div>
                <div className="empty-text">暂无模型数据，等待第一个请求...</div>
              </div>
            )}
            {models.slice(0, 5).map((m) => (
              <div key={m.model} className="model-item" onClick={() => navigate('/models')}>
                <div className="model-icon" style={{ background: providerColor(m.provider || m.model) }}>
                  {m.model.slice(0, 2).toUpperCase()}
                </div>
                <div className="model-info">
                  <div className="model-name">{m.model}</div>
                  <div className="model-provider">{m.provider || '—'} · {formatNumber(m.requests)} 请求</div>
                </div>
                <span className={`model-status ${m.status}`} />
                <div className="model-latency">
                  {m.avg_latency < 1000 ? m.avg_latency.toFixed(0) + 'ms' : (m.avg_latency / 1000).toFixed(1) + 's'}
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="card-panel">
          <div className="panel-header">
            <div className="panel-title">模型用量排行</div>
          </div>
          {models.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">📊</div>
              <div className="empty-text">暂无用量数据</div>
            </div>
          ) : (
            <table className="usage-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>请求数</th>
                  <th>Tokens</th>
                  <th>用量</th>
                </tr>
              </thead>
              <tbody>
                {models
                  .sort((a, b) => b.requests - a.requests)
                  .slice(0, 5)
                  .map((m, i) => {
                    const maxReq = Math.max(...models.map((x) => x.requests), 1)
                    const pct = (m.requests / maxReq) * 100
                    return (
                      <tr key={m.model}>
                        <td>
                          <div className="model-cell">
                            <span className="model-dot" style={{ background: providerColor(m.provider || m.model) }} />
                            {m.model}
                          </div>
                        </td>
                        <td>{formatNumber(m.requests)}</td>
                        <td>{formatNumber(m.total_tokens)}</td>
                        <td>
                          <div className="usage-bar-bg">
                            <div
                              className="usage-bar-fill"
                              style={{
                                width: `${pct}%`,
                                background: providerColor(m.provider || m.model),
                              }}
                            />
                          </div>
                        </td>
                      </tr>
                    )
                  })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </>
  )
}
