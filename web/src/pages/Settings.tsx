import PageHeader from '../components/PageHeader'

export default function Settings() {
  return (
    <>
      <PageHeader title="设置" subtitle="网关配置与管理" />
      <div className="settings-group">
        <div className="sg-title">通用</div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #9a3412, #d97706)' }}>⚙</div>
          <div className="si-info">
            <div className="si-label">网关设置</div>
            <div className="si-desc">端口、日志级别、超时配置</div>
          </div>
        </div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #059669, #34d399)' }}>🔑</div>
          <div className="si-info">
            <div className="si-label">API Keys</div>
            <div className="si-desc">管理访问密钥</div>
          </div>
        </div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #6366f1, #818cf8)' }}>🔌</div>
          <div className="si-info">
            <div className="si-label">提供商管理</div>
            <div className="si-desc">添加、配置、启用/禁用</div>
          </div>
        </div>
      </div>
      <div className="settings-group">
        <div className="sg-title">数据</div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #d97706, #f59e0b)' }}>📊</div>
          <div className="si-info">
            <div className="si-label">用量与计费</div>
            <div className="si-desc">Token 统计、请求量报表</div>
          </div>
        </div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #dc2626, #f87171)' }}>📄</div>
          <div className="si-info">
            <div className="si-label">日志导出</div>
            <div className="si-desc">JSON / CSV 格式</div>
          </div>
        </div>
      </div>
      <div className="settings-group">
        <div className="sg-title">关于</div>
        <div className="settings-item">
          <div className="si-icon" style={{ background: 'linear-gradient(135deg, #7c3aed, #a78bfa)' }}>ℹ️</div>
          <div className="si-info">
            <div className="si-label">版本信息</div>
            <div className="si-desc">go-llm-gateway · litellm-admin v1.0.0</div>
          </div>
        </div>
      </div>
    </>
  )
}
