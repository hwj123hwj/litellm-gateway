import { useEffect, useState } from 'react'
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
  const {
    providers,
    providersLoading,
    providersError,
    fetchProviders,
    routes,
    routesError,
    fetchRoutes,
    updateProvider,
    resetProvider,
    checkProvider,
    updateRoute,
  } = useStore()
  const [pending, setPending] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  useEffect(() => {
    fetchProviders()
    fetchRoutes()
    const timer = setInterval(() => {
      fetchProviders()
      fetchRoutes()
    }, 15000)
    return () => clearInterval(timer)
  }, [fetchProviders, fetchRoutes])

  if (providersLoading && !providers) return <div className="loading">加载中...</div>
  if (providersError) return <div className="error-banner">⚠ {providersError}</div>
  if (!providers) return null

  const runAction = async (key: string, action: () => Promise<void>) => {
    setPending(key)
    setActionError(null)
    try {
      await action()
    } catch (e: any) {
      setActionError(e?.message || '操作失败')
    } finally {
      setPending(null)
    }
  }

  const moveProvider = (model: string, names: string[], index: number, delta: number) => {
    const nextIndex = index + delta
    if (nextIndex < 0 || nextIndex >= names.length) return
    const next = [...names]
    ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
    void runAction(`route:${model}`, () => updateRoute(model, next))
  }

  const statusLabel = (status: string, state?: string) => {
    if (status === 'online') return '● 在线'
    if (status === 'degraded') return '● 降级'
    if (status === 'offline') return '● 离线'
    if (state === 'half_open') return '◐ 探测中'
    return '● 未知'
  }

  return (
    <>
      <PageHeader title="提供商" subtitle={`共 ${providers.total} 个提供商`} />
      {actionError && <div className="error-banner">⚠ {actionError}</div>}
      <div className="provider-grid">
        {providers.providers.map((p) => (
          <div key={p.name} className="provider-card">
            <div className="provider-icon" style={{ background: `linear-gradient(135deg, ${color(p.name)}, ${color(p.name)}cc)` }}>
              {abbr(p.name)}
            </div>
            <div className="provider-name">{p.name}</div>
            <span className={`provider-indicator ${p.status}`}>
              {statusLabel(p.status, p.state)}
            </span>
            <div style={{ fontSize: 11, color: '#78716c', marginTop: 6 }}>
              熔断：{p.state === 'open' ? '开启' : p.state === 'half_open' ? '半开探测' : '关闭'}
              {p.consecutive_failures ? ` · 连续失败 ${p.consecutive_failures}` : ''}
            </div>
            {p.requests > 0 && (
              <div style={{ fontSize: 11, color: '#a8a29e', marginTop: 6 }}>
                {p.requests} 请求 · {p.avg_latency < 1000 ? p.avg_latency.toFixed(0) + 'ms' : (p.avg_latency / 1000).toFixed(1) + 's'}
              </div>
            )}
            <div style={{ display: 'flex', gap: 6, marginTop: 10, flexWrap: 'wrap', justifyContent: 'center' }}>
              <button
                type="button"
                disabled={pending === `provider:${p.name}`}
                onClick={() => void runAction(`provider:${p.name}`, () => updateProvider(p.name, p.enabled === false))}
              >
                {p.enabled === false ? '启用' : '停用'}
              </button>
              <button type="button" disabled={pending === `check:${p.name}`} onClick={() => void runAction(`check:${p.name}`, () => checkProvider(p.name))}>
                探测
              </button>
              <button type="button" disabled={pending === `reset:${p.name}`} onClick={() => void runAction(`reset:${p.name}`, () => resetProvider(p.name))}>
                重置熔断
              </button>
            </div>
          </div>
        ))}
        {providers.providers.length === 0 && (
          <div className="empty-state" style={{ gridColumn: '1 / -1' }}>
            <div className="empty-icon">🔌</div>
            <div className="empty-text">暂无提供商数据</div>
          </div>
        )}
      </div>

      <div style={{ marginTop: 28 }}>
        <PageHeader title="故障转移顺序" subtitle="上下移动即可调整同一模型的 Provider 优先级" />
        {routesError && <div className="error-banner">⚠ {routesError}</div>}
        <div className="card-panel">
          {!routes || routes.routes.length === 0 ? (
            <div className="empty-state">暂无路由链</div>
          ) : routes.routes.map((route) => {
            const names = route.providers.map((item) => item.name)
            return (
              <div key={route.model} style={{ padding: '12px 0', borderBottom: '1px solid #e7e5e4' }}>
                <div style={{ fontWeight: 600, marginBottom: 8 }}>{route.model}</div>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  {route.providers.map((item, index) => (
                    <div key={`${route.model}:${item.name}`} style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '5px 8px', border: '1px solid #e7e5e4', borderRadius: 8 }}>
                      <span>{index + 1}. {item.name}</span>
                      <span style={{ color: item.status === 'online' ? '#059669' : item.status === 'degraded' ? '#d97706' : '#a8a29e', fontSize: 11 }}>{statusLabel(item.status, item.state)}</span>
                      <button type="button" disabled={index === 0 || pending === `route:${route.model}`} onClick={() => moveProvider(route.model, names, index, -1)}>↑</button>
                      <button type="button" disabled={index === names.length - 1 || pending === `route:${route.model}`} onClick={() => moveProvider(route.model, names, index, 1)}>↓</button>
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </>
  )
}
