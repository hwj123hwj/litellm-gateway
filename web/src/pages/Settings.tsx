import { useState } from 'react'
import { ChartBar, Globe, Info, Key, PlugsConnected, ShieldCheck } from '@phosphor-icons/react'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'

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
