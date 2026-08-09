import { useEffect, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  CheckCircle,
  GearSix,
  PlugsConnected,
  Pulse,
  Question,
  Warning,
  XCircle,
} from '@phosphor-icons/react'
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

  if (providersLoading && !providers) return <div className="loading" role="status">正在读取提供商状态</div>
  if (providersError) return <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{providersError}</div>
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
    if (status === 'online') return '在线'
    if (status === 'degraded') return '降级'
    if (status === 'offline') return '离线'
    if (state === 'half_open') return '探测中'
    return '未知'
  }

  const StatusIcon = ({ status, state }: { status: string; state?: string }) => {
    if (status === 'online') return <CheckCircle size={13} weight="fill" aria-hidden="true" />
    if (status === 'degraded' || state === 'half_open') return <Warning size={13} weight="fill" aria-hidden="true" />
    if (status === 'offline') return <XCircle size={13} weight="fill" aria-hidden="true" />
    return <Question size={13} weight="bold" aria-hidden="true" />
  }

  return (
    <>
      <PageHeader title="提供商" subtitle={`共 ${providers.total} 个提供商`} />
      {actionError && <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{actionError}</div>}
      <div className="provider-grid">
        {providers.providers.map((p) => (
          <article key={p.name} className="provider-card">
            <div className="provider-card-top">
              <div className="provider-icon" style={{ background: color(p.name) }}>
                <PlugsConnected size={20} weight="duotone" aria-hidden="true" />
              </div>
              <span className={`provider-indicator ${p.status}`}>
                <StatusIcon status={p.status} state={p.state} />
                {statusLabel(p.status, p.state)}
              </span>
            </div>
            <div className="provider-name">{p.name}</div>
            <div className="provider-meta">
              <span>熔断 {p.state === 'open' ? '开启' : p.state === 'half_open' ? '半开探测' : '关闭'}</span>
              {p.consecutive_failures ? <span>连续失败 {p.consecutive_failures}</span> : null}
            </div>
            {p.requests > 0 && (
              <div className="provider-traffic">
                <Pulse size={13} weight="duotone" aria-hidden="true" />
                {p.requests} 请求 / {p.avg_latency < 1000 ? p.avg_latency.toFixed(0) + 'ms' : (p.avg_latency / 1000).toFixed(1) + 's'}
              </div>
            )}
            <div className="provider-actions">
              <button
                className="button button-secondary"
                type="button"
                disabled={pending === `provider:${p.name}`}
                onClick={() => void runAction(`provider:${p.name}`, () => updateProvider(p.name, p.enabled === false))}
              >
                <GearSix size={14} weight="bold" aria-hidden="true" />{p.enabled === false ? '启用' : '停用'}
              </button>
              <button className="button button-ghost" type="button" disabled={pending === `check:${p.name}`} onClick={() => void runAction(`check:${p.name}`, () => checkProvider(p.name))}>
                探测
              </button>
              <button className="button button-ghost" type="button" disabled={pending === `reset:${p.name}`} onClick={() => void runAction(`reset:${p.name}`, () => resetProvider(p.name))}>
                重置
              </button>
            </div>
          </article>
        ))}
        {providers.providers.length === 0 && (
          <div className="empty-state provider-empty">
            <div className="empty-icon"><PlugsConnected size={36} weight="duotone" aria-hidden="true" /></div>
            <div className="empty-text">暂无提供商数据</div>
          </div>
        )}
      </div>

      <div className="route-section">
        <PageHeader title="故障转移顺序" subtitle="上下移动即可调整同一模型的 Provider 优先级" />
        {routesError && <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{routesError}</div>}
        <div className="card-panel">
          {!routes || routes.routes.length === 0 ? (
            <div className="empty-state">暂无路由链</div>
          ) : routes.routes.map((route) => {
            const names = route.providers.map((item) => item.name)
            return (
              <div key={route.model} className="route-row">
                <div className="route-model">{route.model}</div>
                <div className="route-providers">
                  {route.providers.map((item, index) => (
                    <div key={`${route.model}:${item.name}`} className="route-provider">
                      <span className="route-provider-index">{index + 1}</span>
                      <span className="route-provider-name">{item.name}</span>
                      <span className={`route-provider-status ${item.status}`}><StatusIcon status={item.status} state={item.state} />{statusLabel(item.status, item.state)}</span>
                      <button className="icon-btn compact" type="button" aria-label={`将 ${item.name} 上移`} disabled={index === 0 || pending === `route:${route.model}`} onClick={() => moveProvider(route.model, names, index, -1)}><ArrowUp size={14} weight="bold" aria-hidden="true" /></button>
                      <button className="icon-btn compact" type="button" aria-label={`将 ${item.name} 下移`} disabled={index === names.length - 1 || pending === `route:${route.model}`} onClick={() => moveProvider(route.model, names, index, 1)}><ArrowDown size={14} weight="bold" aria-hidden="true" /></button>
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
