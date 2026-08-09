import { useEffect, useState } from 'react'
import { Check, Cube, FloppyDisk, Plus, Warning, X } from '@phosphor-icons/react'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'

const COLORS: Record<string, string> = {
  glm: '#9a3412', mimo: '#059669', longcat: '#6366f1',
  easyclaw: '#dc2626', deepv: '#2563eb', copilot: '#7c3aed',
}

const CAPABILITIES = ['text', 'vision', 'video', 'file', 'audio', 'tool_calling', 'streaming', 'reasoning']

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
  const { models, modelsLoading, modelsError, fetchModels, updateModel } = useStore()
  const [drafts, setDrafts] = useState<Record<string, string[]>>({})
  const [saving, setSaving] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    fetchModels()
    const timer = setInterval(fetchModels, 15000)
    return () => clearInterval(timer)
  }, [fetchModels])

  if (modelsLoading && !models) return <div className="loading" role="status">正在读取模型配置</div>
  if (modelsError) return <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{modelsError}</div>
  if (!models) return null

  const toggleCapability = (model: string, capability: string, current: string[]) => {
    const next = current.includes(capability)
      ? current.filter((item) => item !== capability)
      : [...current, capability]
    setDrafts((prev) => ({ ...prev, [model]: next }))
  }

  const saveCapabilities = async (model: string, capabilities: string[], modalities?: string[]) => {
    setSaving(model)
    setSaveError(null)
    try {
      await updateModel(model, capabilities, modalities)
      setDrafts((prev) => {
        const next = { ...prev }
        delete next[model]
        return next
      })
    } catch (e: any) {
      setSaveError(e?.message || '模型能力更新失败')
    } finally {
      setSaving(null)
    }
  }

  const activeCount = models.models.filter((m) => m.status !== 'idle').length

  return (
    <>
      <PageHeader title="模型管理" subtitle={`共 ${models.total} 个模型，${activeCount} 个活跃`} />
      {saveError && <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{saveError}</div>}
      <div className="model-detail-list">
        {models.models
          .slice()
          .sort((a, b) => b.requests - a.requests)
          .map((m) => (
            <article key={m.model} className="model-detail-card">
              <div className="md-header">
                <div className="md-icon" style={{ background: color(m.provider || m.model) }}>
                  <Cube size={20} weight="duotone" aria-hidden="true" />
                </div>
                <div>
                  <div className="md-name">{m.model}</div>
                  <div className="md-provider">{m.provider || 'N/A'}</div>
                </div>
                <span className={`model-status-badge ${m.status}`}>
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
              <div className="capability-section">
                <div className="capability-label">能力校验</div>
                <div className="capability-list">
                  {(drafts[m.model] || m.capabilities || []).map((capability) => (
                    <button
                      key={`${m.model}:${capability}`}
                      type="button"
                      className="capability-chip selected"
                      onClick={() => toggleCapability(m.model, capability, drafts[m.model] || m.capabilities || [])}
                      aria-label={`移除 ${capability} 能力`}
                    >
                      <Check size={13} weight="bold" aria-hidden="true" />{capability}<X size={12} weight="bold" aria-hidden="true" />
                    </button>
                  ))}
                  {CAPABILITIES.filter((capability) => !(drafts[m.model] || m.capabilities || []).includes(capability)).map((capability) => (
                    <button
                      key={`${m.model}:add:${capability}`}
                      type="button"
                      className="capability-chip available"
                      onClick={() => toggleCapability(m.model, capability, drafts[m.model] || m.capabilities || [])}
                    >
                      <Plus size={13} weight="bold" aria-hidden="true" />{capability}
                    </button>
                  ))}
                </div>
                {drafts[m.model] && (
                  <button
                    type="button"
                    className="button button-primary save-capabilities"
                    disabled={saving === m.model}
                    onClick={() => saveCapabilities(m.model, drafts[m.model])}
                  >
                    <FloppyDisk size={15} weight="bold" aria-hidden="true" />{saving === m.model ? '保存中...' : '保存能力'}
                  </button>
                )}
              </div>
            </article>
          ))}
        {models.models.length === 0 && (
          <div className="empty-state">
            <div className="empty-icon"><Cube size={36} weight="duotone" aria-hidden="true" /></div>
            <div className="empty-text">暂无模型数据</div>
          </div>
        )}
      </div>
    </>
  )
}
