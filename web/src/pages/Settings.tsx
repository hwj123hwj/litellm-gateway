import { useCallback, useEffect, useState } from 'react'
import {
  ChartBar,
  CheckCircle,
  Globe,
  Info,
  Key,
  PlugsConnected,
  ShieldCheck,
  WarningCircle,
} from '@phosphor-icons/react'
import { getPiConfig, syncPiConfig } from '../api'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'
import type { PiConfigResponse } from '../api/types'

export default function Settings() {
  const { backendUrl, setBackendUrl, apiKey, setApiKey } = useStore()
  const [urlInput, setUrlInput] = useState(backendUrl)
  const [saved, setSaved] = useState(false)
  const [piConfig, setPiConfig] = useState<PiConfigResponse | null>(null)
  const [piSyncing, setPiSyncing] = useState(false)
  const [piSynced, setPiSynced] = useState(false)
  const [piError, setPiError] = useState('')

  const refreshPiConfig = useCallback(() => {
    if (!apiKey) return
    getPiConfig()
      .then(setPiConfig)
      .catch(() => setPiConfig(null))
  }, [apiKey])

  useEffect(() => {
    refreshPiConfig()
  }, [refreshPiConfig])

  const handleSaveUrl = () => {
    setBackendUrl(urlInput)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const handleSyncPi = async () => {
    setPiSyncing(true)
    setPiError('')
    try {
      const next = await syncPiConfig()
      setPiConfig(next)
      setPiSynced(true)
      setTimeout(() => setPiSynced(false), 2000)
    } catch (err) {
      setPiError(err instanceof Error ? err.message : '同步失败')
    } finally {
      setPiSyncing(false)
    }
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

      <div className="settings-group">
        <div className="sg-title">Pi 集成</div>
        <div className="settings-item settings-stack">
          <div className="settings-heading">
            <div className="si-icon success"><PlugsConnected size={18} weight="duotone" aria-hidden="true" /></div>
            <div className="si-info">
              <div className="si-label">Pi 模型清单</div>
              <div className="si-desc">
                把网关精选的模型列表写入 Pi 的 models.json（~/.pi/agent）
              </div>
            </div>
            {piConfig && (
              <span
                className={`status-badge ${piConfig.in_sync ? 'ok' : 'degraded'}`}
                style={{ marginLeft: 'auto' }}
              >
                {piConfig.in_sync ? (
                  <><CheckCircle size={13} weight="bold" aria-hidden="true" /> 已同步</>
                ) : (
                  <><WarningCircle size={13} weight="bold" aria-hidden="true" /> 待同步</>
                )}
              </span>
            )}
          </div>
          {piConfig ? (
            <>
              <div className="settings-current">{piConfig.path}</div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {piConfig.desired.map((m) => {
                  const missing = piConfig.missing_ids.includes(m.id)
                  return (
                    <span
                      key={m.id}
                      className={`capability-chip ${missing ? 'available' : 'selected'}`}
                      style={{ cursor: 'default' }}
                      title={missing ? 'Pi 中缺失，同步后生效' : m.name}
                    >
                      {m.id}
                    </span>
                  )
                })}
              </div>
              {piConfig.stale_ids.length > 0 && (
                <div className="settings-current">
                  Pi 中多出（同步时将移除）：{piConfig.stale_ids.join('、')}
                </div>
              )}
              {piError && <div className="settings-current">同步失败：{piError}</div>}
              <div className="settings-control-row">
                <button
                  className={`button ${piSynced ? 'button-success' : 'button-primary'}`}
                  disabled={piSyncing}
                  onClick={handleSyncPi}
                >
                  {piSynced ? (
                    <><ShieldCheck size={15} weight="bold" aria-hidden="true" />已同步</>
                  ) : piSyncing ? '同步中…' : '同步到 Pi'}
                </button>
                <span className="si-desc" style={{ alignSelf: 'center' }}>
                  同步前自动备份为 models.json.pre-sync.bak；完成后重启 Pi 生效
                </span>
              </div>
            </>
          ) : (
            <div className="settings-current">
              {apiKey ? '无法读取 Pi 集成状态（请检查后端地址与 API Key）' : '填写 API Key 后可管理 Pi 模型清单'}
            </div>
          )}
        </div>
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
