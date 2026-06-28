interface Props {
  status: string
  onMenuClick: () => void
}

export default function Header({ status, onMenuClick }: Props) {
  const statusClass = status === 'ok' ? 'ok' : status === 'degraded' ? 'degraded' : 'error'
  const statusText = status === 'ok' ? '所有系统正常' : status === 'degraded' ? '部分降级' : '未连接'

  return (
    <header className="header">
      <div className="header-left">
        <button className="icon-btn mobile-menu-btn" onClick={onMenuClick}>
          ☰
        </button>
        <div>
          <div className="breadcrumb">
            <span>Home</span><span>›</span><span>Dashboard</span>
          </div>
          <h2>LiteLLM Gateway</h2>
        </div>
      </div>
      <div className="header-right">
        <div className={`status-badge ${statusClass}`}>
          <span className={`status-dot ${statusClass}`} />
          {statusText}
        </div>
        <button className="icon-btn" onClick={() => window.location.reload()} title="刷新">
          🔄
        </button>
      </div>
    </header>
  )
}
