import { NavLink, useLocation } from 'react-router-dom'
import {
  ChartLineUp,
  Cube,
  FileText,
  Gear,
  House,
  PlugsConnected,
  ShieldCheck,
  X,
} from '@phosphor-icons/react'

const NAV_ITEMS = [
  { path: '/', label: 'Dashboard', icon: House },
  { path: '/models', label: 'Models', icon: Cube },
  { path: '/providers', label: 'Providers', icon: PlugsConnected },
  { path: '/logs', label: 'Logs', icon: FileText },
  { path: '/settings', label: 'Settings', icon: Gear },
]

interface Props {
  open?: boolean
  onClose?: () => void
}

export default function Sidebar({ open = false, onClose }: Props) {
  const location = useLocation()

  return (
    <>
      <button
        className={`sidebar-overlay${open ? ' show' : ''}`}
        type="button"
        aria-label="关闭导航"
        onClick={onClose}
      />
      <aside className={`sidebar${open ? ' open' : ''}`}>
        {onClose && (
          <button className="sidebar-close" type="button" aria-label="关闭导航" onClick={onClose}>
            <X size={18} weight="bold" />
          </button>
        )}
      <div className="sidebar-logo">
        <div className="logo-icon">G</div>
        <div>
          <h1>LiteLLM</h1>
          <span>Gateway control plane</span>
        </div>
      </div>

      <nav>
        {NAV_ITEMS.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
            onClick={onClose}
            end={item.path === '/'}
          >
            <item.icon size={18} weight={location.pathname === item.path ? 'fill' : 'regular'} aria-hidden="true" />
            {item.label}
          </NavLink>
        ))}
      </nav>

      <div className="sidebar-footer">
        <div className="user-card">
          <div className="avatar"><ShieldCheck size={18} weight="duotone" aria-hidden="true" /></div>
          <div className="user-info">
            <div className="user-name">Admin</div>
            <div className="user-role">local workspace</div>
          </div>
          <ChartLineUp size={16} className="user-health" aria-hidden="true" />
        </div>
      </div>
      </aside>
    </>
  )
}
