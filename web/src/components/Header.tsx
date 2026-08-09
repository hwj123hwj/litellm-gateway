import { ArrowClockwise, List, Warning, CheckCircle, XCircle } from '@phosphor-icons/react'

interface Props {
  status: string
  onMenuClick: () => void
}

export default function Header({ status, onMenuClick }: Props) {
  const statusClass = status === 'ok' ? 'ok' : status === 'degraded' ? 'degraded' : 'error'
  const statusText = status === 'ok' ? 'All systems operational' : status === 'degraded' ? 'Partial degradation' : 'Gateway unavailable'
  const StatusIcon = status === 'ok' ? CheckCircle : status === 'degraded' ? Warning : XCircle

  return (
    <header className="header">
      <div className="header-left">
        <button className="icon-btn mobile-menu-btn" onClick={onMenuClick} aria-label="打开导航">
          <List size={20} weight="bold" aria-hidden="true" />
        </button>
        <div>
          <div className="breadcrumb">
            <span>Workspace</span><span>/</span><span>Gateway</span>
          </div>
          <h2>Control plane</h2>
        </div>
      </div>
      <div className="header-right">
        <div className={`status-badge ${statusClass}`}>
          <StatusIcon size={15} weight="fill" aria-hidden="true" />
          {statusText}
        </div>
        <button className="icon-btn" onClick={() => window.location.reload()} title="刷新页面" aria-label="刷新页面">
          <ArrowClockwise size={18} weight="bold" aria-hidden="true" />
        </button>
      </div>
    </header>
  )
}
