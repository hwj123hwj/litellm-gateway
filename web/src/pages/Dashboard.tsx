import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ArrowRight,
  ArrowUp,
  ChartBar,
  ChartLineUp,
  CheckCircle,
  Cube,
  Mailbox,
  Timer,
  Warning,
} from '@phosphor-icons/react'
import { useStore } from '../store'

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

function formatLatency(ms: number): string {
  return ms < 1000 ? ms.toFixed(0) + 'ms' : (ms / 1000).toFixed(1) + 's'
}

function dashboardStatus(todayRequests: number, successRate: number): 'idle' | 'ok' | 'degraded' | 'error' {
  if (todayRequests === 0) return 'idle'
  if (successRate >= 99) return 'ok'
  if (successRate >= 95) return 'degraded'
  return 'error'
}

export default function Dashboard() {
  const navigate = useNavigate()
  const { dashboard, dashboardLoading, dashboardError, fetchDashboard } = useStore()

  useEffect(() => {
    fetchDashboard()
    const timer = setInterval(fetchDashboard, 10000)
    return () => clearInterval(timer)
  }, [fetchDashboard])

  if (dashboardLoading && !dashboard) return <div className="loading" role="status">正在读取网关指标</div>
  if (dashboardError) return <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{dashboardError}</div>
  if (!dashboard) return null

  const { summary, providers, models } = dashboard
  const hasRequests = summary.today_requests > 0
  const status = dashboardStatus(summary.today_requests, summary.success_rate)
  const sortedModels = models.slice().sort((a, b) => b.requests - a.requests)
  const statusText = status === 'idle'
    ? '暂无业务请求'
    : status === 'ok'
      ? '所有系统正常运行'
      : status === 'degraded'
        ? '部分请求失败，请关注上游状态'
        : '请求失败率偏高，请检查上游配置'

  return (
    <>
      {/* Status row */}
      <div className={`status-row ${status}`}>
        <ChartLineUp className="status-row-icon" size={18} weight="duotone" aria-hidden="true" />
        <span className="status-text">
          {statusText}
        </span>
        <span className="status-time">运行 {summary.uptime}</span>
      </div>

      {/* KPI */}
      <div className="kpi-grid">
        <div className="kpi-card">
          <div className="kpi-label"><ChartBar size={16} weight="duotone" aria-hidden="true" />今日请求</div>
          <div className="kpi-value">{formatNumber(summary.today_requests)}</div>
        </div>
        <div className="kpi-card">
          <div className="kpi-label"><CheckCircle size={16} weight="duotone" aria-hidden="true" />成功率</div>
          <div className="kpi-value">{hasRequests ? summary.success_rate.toFixed(1) + '%' : 'N/A'}</div>
          {hasRequests && (
            <div className={`kpi-trend ${summary.success_rate >= 99 ? 'up' : summary.success_rate >= 95 ? 'neutral' : 'down'}`}>
              {summary.success_rate >= 99 ? <><ArrowUp size={14} weight="bold" />优秀</> : summary.success_rate >= 95 ? '正常' : '偏低'}
            </div>
          )}
        </div>
        <div className="kpi-card">
          <div className="kpi-label"><Cube size={16} weight="duotone" aria-hidden="true" />活跃模型</div>
          <div className="kpi-value">{summary.active_models}</div>
        </div>
        <div className="kpi-card">
          <div className="kpi-label"><Timer size={16} weight="duotone" aria-hidden="true" />平均延迟</div>
          <div className="kpi-value">{hasRequests ? formatLatency(summary.avg_latency_ms) : 'N/A'}</div>
          {hasRequests && (
            <div className={`kpi-trend ${summary.avg_latency_ms < 2000 ? 'up' : summary.avg_latency_ms < 5000 ? 'neutral' : 'down'}`}>
              {summary.avg_latency_ms < 2000 ? '正常' : '偏高'}
            </div>
          )}
        </div>
      </div>

      {/* Provider strip */}
      {providers.length > 0 && (
        <div className="provider-strip">
          {providers.map((p) => (
            <button
              key={p.name}
              type="button"
              className="provider-chip"
              onClick={() => navigate('/providers')}
            >
              <span className={`p-dot ${p.status}`} />
              <span className="p-label">{p.name}</span>
              <span className={`p-badge ${p.status}`}>
                {p.requests > 0 ? formatLatency(p.avg_latency) : '未使用'}
              </span>
            </button>
          ))}
        </div>
      )}

      {/* Models + Chart */}
      <div className="grid-2col">
        <div className="card-panel">
          <div className="panel-header">
            <div className="panel-title">活跃模型（今日）</div>
            <button className="panel-action" type="button" onClick={() => navigate('/models')}>查看全部 <ArrowRight size={15} weight="bold" aria-hidden="true" /></button>
          </div>
          <div className="model-list">
            {models.length === 0 && (
              <div className="empty-state">
                <div className="empty-icon"><Mailbox size={36} weight="duotone" aria-hidden="true" /></div>
                <div className="empty-text">暂无业务请求，模型使用数据将在首个请求后显示</div>
              </div>
            )}
            {sortedModels.slice(0, 5).map((m) => (
              <button key={m.model} type="button" className="model-item" onClick={() => navigate('/models')}>
                <div className="model-icon" style={{ background: providerColor(m.provider || m.model) }}>
                  {m.model.slice(0, 2).toUpperCase()}
                </div>
                <div className="model-info">
                  <div className="model-name">{m.model}</div>
                  <div className="model-provider">{m.provider || 'N/A'} / {formatNumber(m.requests)} 请求</div>
                </div>
                <span className={`model-status ${m.status}`} />
                <div className="model-latency">
                  {m.requests > 0 ? formatLatency(m.avg_latency) : 'N/A'}
                </div>
              </button>
            ))}
          </div>
        </div>

        <div className="card-panel">
          <div className="panel-header">
            <div className="panel-title">今日模型用量排行</div>
          </div>
          {models.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon"><ChartBar size={36} weight="duotone" aria-hidden="true" /></div>
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
                {sortedModels
                  .slice(0, 5)
                  .map((m) => {
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
