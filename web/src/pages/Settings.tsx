import { useCallback, useEffect, useState } from 'react'
import {
  ChartBar,
  CheckCircle,
  Globe,
  Info,
  Key,
  ChatCircleDots,
  PlugsConnected,
  ShieldCheck,
  ThumbsDown,
  ThumbsUp,
  WarningCircle,
} from '@phosphor-icons/react'
import {
  getAssistantPrompt,
  getHarnessConfig,
  getPiConfig,
  getZCodeConfig,
  listAssistantFeedback,
  setAssistantPrompt,
  syncHarnessConfig,
  syncPiConfig,
  syncZCodeConfig,
} from '../api'
import type { AssistantFeedbackEntry } from '../api/types'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'
import type { PiConfigResponse } from '../api/types'

// 三张「同步模型清单到客户端」卡片共用一套状态与交互。
const SYNC_TARGETS = [
  {
    key: 'pi',
    title: 'EasyAgent 模型清单',
    desc: '把网关精选的模型列表写入 EasyAgent 的 models.json（~/.easyagent，兼容旧 ~/.pi/agent）',
    backupNote: '同步前自动备份为 models.json.pre-sync.bak；完成后重启 EasyAgent 生效',
    buttonLabel: '同步到 EasyAgent',
    getConfig: getPiConfig,
    syncConfig: syncPiConfig,
  },
  {
    key: 'zcode',
    title: 'ZCode 模型清单',
    desc: '更新 ZCode 的网关 provider 规则模型列表（~/.zcode/v2/provider_config.json）',
    backupNote: '同步前自动备份为 provider_config.json.pre-sync.bak；只改网关规则的模型列表',
    buttonLabel: '同步到 ZCode',
    getConfig: getZCodeConfig,
    syncConfig: syncZCodeConfig,
  },
  {
    key: 'harness',
    title: 'DeepSeek Harness 模型清单',
    desc: '更新 harness 的 llm-deepseek / llm-pi-ai 条目模型列表（~/.dsh/profiles/desktop/cordis.patch.yml）',
    backupNote: '同步前自动备份为 cordis.patch.yml.pre-sync.bak；缺失的条目会跳过',
    buttonLabel: '同步到 Harness',
    getConfig: getHarnessConfig,
    syncConfig: syncHarnessConfig,
  },
] as const

interface SyncCardProps {
  target: (typeof SYNC_TARGETS)[number]
  apiKey: string
}

function SyncCard({ target, apiKey }: SyncCardProps) {
  const [config, setConfig] = useState<PiConfigResponse | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [synced, setSynced] = useState(false)
  const [error, setError] = useState('')

  const refresh = useCallback(() => {
    if (!apiKey) return
    target
      .getConfig()
      .then(setConfig)
      .catch(() => setConfig(null))
  }, [apiKey, target])

  useEffect(() => {
    refresh()
  }, [refresh])

  const handleSync = async () => {
    setSyncing(true)
    setError('')
    try {
      const next = await target.syncConfig()
      setConfig(next)
      setSynced(true)
      setTimeout(() => setSynced(false), 2000)
    } catch (err) {
      setError(err instanceof Error ? err.message : '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  const skippedEntries = (config as { skipped_entries?: string[] } | null)?.skipped_entries

  return (
    <div className="settings-item settings-stack">
      <div className="settings-heading">
        <div className="si-icon success"><PlugsConnected size={18} weight="duotone" aria-hidden="true" /></div>
        <div className="si-info">
          <div className="si-label">{target.title}</div>
          <div className="si-desc">{target.desc}</div>
        </div>
        {config && (
          <span
            className={`status-badge ${config.in_sync ? 'ok' : 'degraded'}`}
            style={{ marginLeft: 'auto' }}
          >
            {config.in_sync ? (
              <><CheckCircle size={13} weight="bold" aria-hidden="true" /> 已同步</>
            ) : (
              <><WarningCircle size={13} weight="bold" aria-hidden="true" /> 待同步</>
            )}
          </span>
        )}
      </div>
      {config ? (
        <>
          <div className="settings-current">{config.path}</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {config.desired.map((m) => {
              const missing = config.missing_ids.includes(m.id)
              return (
                <span
                  key={m.id}
                  className={`capability-chip ${missing ? 'available' : 'selected'}`}
                  style={{ cursor: 'default' }}
                  title={missing ? '客户端缺失，同步后生效' : m.name}
                >
                  {m.id}
                </span>
              )
            })}
          </div>
          {config.stale_ids.length > 0 && (
            <div className="settings-current">
              客户端多出（同步时将移除）：{config.stale_ids.join('、')}
            </div>
          )}
          {skippedEntries && skippedEntries.length > 0 && (
            <div className="settings-current">
              跳过缺失条目：{skippedEntries.join('、')}（对应 bundle 未配置，不盲建）
            </div>
          )}
          {error && <div className="settings-current">同步失败：{error}</div>}
          <div className="settings-control-row">
            <button
              className={`button ${synced ? 'button-success' : 'button-primary'}`}
              disabled={syncing}
              onClick={handleSync}
            >
              {synced ? (
                <><ShieldCheck size={15} weight="bold" aria-hidden="true" />已同步</>
              ) : syncing ? '同步中…' : target.buttonLabel}
            </button>
            <span className="si-desc" style={{ alignSelf: 'center' }}>
              {target.backupNote}
            </span>
          </div>
        </>
      ) : (
        <div className="settings-current">
          {apiKey ? '无法读取集成状态（请检查后端地址与 API Key）' : '填写 API Key 后可管理模型清单'}
        </div>
      )}
    </div>
  )
}


// 助理人设：编辑 system prompt（热更新，空串恢复内置默认）+ 反馈复盘列表。
function AssistantPersona() {
  const { apiKey } = useStore()
  const [prompt, setPrompt] = useState('')
  const [custom, setCustom] = useState('')
  const [defaultPrompt, setDefaultPrompt] = useState('')
  const [feedback, setFeedback] = useState<AssistantFeedbackEntry[]>([])
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  const refresh = useCallback(() => {
    if (!apiKey) return
    getAssistantPrompt()
      .then((r) => {
        setPrompt(r.custom || r.default)
        setCustom(r.custom)
        setDefaultPrompt(r.default)
        setDirty(false)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
    listAssistantFeedback(20)
      .then((r) => setFeedback(r.feedback))
      .catch(() => setFeedback([]))
  }, [apiKey])

  useEffect(() => {
    refresh()
  }, [refresh])

  const handleSave = async () => {
    setSaving(true)
    setError('')
    try {
      // 输入与内置默认一致 = 恢复默认（清空自定义）。
      const r = await setAssistantPrompt(prompt.trim() === defaultPrompt.trim() ? '' : prompt)
      setCustom(r.custom)
      setPrompt(r.custom || r.default)
      setSaved(true)
      setDirty(false)
      setTimeout(() => setSaved(false), 2000)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="settings-group">
      <div className="sg-title">助理人设</div>
      <div className="settings-item settings-stack">
        <div className="settings-heading">
          <div className="si-icon accent"><ChatCircleDots size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="si-info">
            <div className="si-label">System Prompt（记忆管家的行为准则）</div>
            <div className="si-desc">保存后立即生效，无需重启；与默认一致时视为恢复默认</div>
          </div>
          {custom ? (
            <span className="status-badge ok" style={{ marginLeft: 'auto' }}>
              <><CheckCircle size={13} weight="bold" aria-hidden="true" /> 自定义中</>
            </span>
          ) : (
            <span className="status-badge degraded" style={{ marginLeft: 'auto' }}>内置默认</span>
          )}
        </div>
        {apiKey ? (
          <>
            <textarea
              className="settings-input"
              rows={12}
              spellCheck={false}
              value={prompt}
              onChange={(e) => { setPrompt(e.target.value); setDirty(true) }}
              placeholder={defaultPrompt || '加载中…'}
            />
            {error && <div className="settings-current">操作失败：{error}</div>}
            <div className="settings-control-row">
              <button
                className={`button ${saved ? 'button-success' : 'button-primary'}`}
                disabled={saving || !dirty}
                onClick={handleSave}
              >
                {saved ? '已保存' : saving ? '保存中…' : '保存并生效'}
              </button>
              <button
                className="button button-ghost"
                disabled={saving || !custom}
                onClick={() => { setPrompt(defaultPrompt); setDirty(true) }}
              >
                载入默认
              </button>
            </div>
          </>
        ) : (
          <div className="settings-current">填写 API Key 后可自定义助理人设</div>
        )}
      </div>
      <div className="settings-item settings-stack" style={{ borderTop: '1px solid var(--line)' }}>
        <div className="settings-heading">
          <div className="si-icon indigo"><ThumbsUp size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="si-info">
            <div className="si-label">反馈记录</div>
            <div className="si-desc">在「记忆管家」对话里对回复打分；据规律自己迭代人设</div>
          </div>
        </div>
        {feedback.length === 0 ? (
          <div className="settings-current">暂无反馈</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {feedback.map((f) => (
              <div key={f.id} className="settings-current" style={{ display: 'flex', gap: 8, alignItems: 'flex-start' }}>
                <span style={{ flexShrink: 0 }}>
                  {f.rating === 'up'
                    ? <ThumbsUp size={13} weight="fill" aria-hidden="true" style={{ color: 'var(--green)' }} />
                    : <ThumbsDown size={13} weight="fill" aria-hidden="true" style={{ color: 'var(--red)' }} />}
                </span>
                <span style={{ minWidth: 0 }}>
                  {f.reply_excerpt}
                  {f.note && <span style={{ color: 'var(--text-muted)' }}>（{f.note}）</span>}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export default function Settings() {
  const { backendUrl, setBackendUrl, apiKey, setApiKey } = useStore()
  const [urlInput, setUrlInput] = useState(backendUrl)
  const [saved, setSaved] = useState(false)

  const handleSaveUrl = () => {
    setBackendUrl(urlInput)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  return (
    <>
      <PageHeader title="设置" subtitle="网关配置与管理" />

      <div className="settings-group">
        <div className="sg-title">连接配置</div>
        <div className="settings-item settings-stack">
          <div className="settings-heading">
            <div className="si-icon accent"><Globe size={18} weight="duotone" aria-hidden="true" /></div>
            <div className="si-info">
              <div className="si-label">后端地址</div>
              <div className="si-desc">Gateway API 地址（留空使用默认）</div>
            </div>
          </div>
          <div className="settings-control-row">
            <input
              className="settings-input"
              type="text"
              placeholder="http://your-server:4001"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
            />
            <button
              className={`button ${saved ? 'button-success' : 'button-primary'}`}
              onClick={handleSaveUrl}
            >
              {saved ? <><ShieldCheck size={15} weight="bold" aria-hidden="true" />已保存</> : '保存'}
            </button>
          </div>
          {backendUrl && (
            <div className="settings-current">
              当前：{backendUrl}
            </div>
          )}
        </div>

        <div className="settings-item settings-stack">
          <div className="settings-heading">
            <div className="si-icon success"><Key size={18} weight="duotone" aria-hidden="true" /></div>
            <div className="si-info">
              <div className="si-label">API Key</div>
              <div className="si-desc">LITELLM_MASTER_KEY 或 ADMIN_TOKEN</div>
            </div>
          </div>
          <input
            className="settings-input"
            type="password"
            placeholder="输入 API Key"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
        </div>
      </div>

      <AssistantPersona />

      <div className="settings-group">
        <div className="sg-title">客户端集成</div>
        {SYNC_TARGETS.map((target) => (
          <SyncCard key={target.key} target={target} apiKey={apiKey} />
        ))}
      </div>

      <div className="settings-group">
        <div className="sg-title">通用</div>
        <div className="settings-item">
          <div className="si-icon indigo"><PlugsConnected size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="si-info">
            <div className="si-label">提供商管理</div>
            <div className="si-desc">添加、配置、启用/禁用</div>
          </div>
        </div>
        <div className="settings-item">
          <div className="si-icon amber"><ChartBar size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="si-info">
            <div className="si-label">用量与计费</div>
            <div className="si-desc">Token 统计、请求量报表</div>
          </div>
        </div>
      </div>

      <div className="settings-group">
        <div className="sg-title">关于</div>
        <div className="settings-item">
          <div className="si-icon violet"><Info size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="si-info">
            <div className="si-label">版本信息</div>
            <div className="si-desc">litellm-gateway / admin v1.0.0</div>
          </div>
        </div>
      </div>
    </>
  )
}
