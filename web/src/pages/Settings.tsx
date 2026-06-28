import { useState } from 'react'
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
        <div className="settings-item" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div className="si-icon" style={{ background: 'linear-gradient(135deg, #9a3412, #d97706)' }}>🌐</div>
            <div className="si-info">
              <div className="si-label">后端地址</div>
              <div className="si-desc">Gateway API 地址（留空使用默认）</div>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <input
              type="text"
              placeholder="http://your-server:4001"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
              style={{
                flex: 1,
                padding: '8px 12px',
                border: '1px solid #ede0d8',
                borderRadius: 10,
                fontSize: 13,
                fontFamily: 'inherit',
              }}
            />
            <button
              onClick={handleSaveUrl}
              style={{
                padding: '8px 16px',
                background: saved ? '#059669' : '#9a3412',
                color: 'white',
                border: 'none',
                borderRadius: 10,
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
            >
              {saved ? '✓ 已保存' : '保存'}
            </button>
          </div>
          {backendUrl && (
            <div style={{ fontSize: 11, color: '#a8a29e' }}>
              当前: {backendUrl}
            </div>
          )}
        </div>

        <div className="settings-item" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div className="si-icon" style={{ background: 'linear-gradient(135deg, #059669, #34d399)' }}>🔑</div>
            <div className="si-info">
              <div className="si-label">API Key</div>
              <div className="si-desc">LITELLM_MASTER_KEY 或 ADMIN_TOKEN</div>
            </div>
          </div>
          <input
            type="password"
            placeholder="输入 API Key"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            style={{
              padding: '8px 12px',
              border: '1px solid #ede0d8',
              borderRadius: 10,
              fontSize: 13,
              fontFamily: 'inherit',
            }}
          />
        </div>
      </div>

      <div className="settings-group">
        <div className="sg-title">通用</div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #6366f1, #818cf8)' }}>🔌</div>
          <div className="si-info">
            <div className="si-label">提供商管理</div>
            <div className="si-desc">添加、配置、启用/禁用</div>
          </div>
        </div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #d97706, #f59e0b)' }}>📊</div>
          <div className="si-info">
            <div className="si-label">用量与计费</div>
            <div className="si-desc">Token 统计、请求量报表</div>
          </div>
        </div>
      </div>

      <div className="settings-group">
        <div className="sg-title">关于</div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #7c3aed, #a78bfa)' }}>ℹ️</div>
          <div className="si-info">
            <div className="si-label">版本信息</div>
            <div className="si-desc">litellm-gateway · admin v1.0.0</div>
          </div>
        </div>
      </div>
    </>
  )
}
