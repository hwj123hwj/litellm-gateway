import { NavLink, useLocation } from 'react-router-dom'

const NAV_ITEMS = [
  { path: '/', label: 'Dashboard', icon: '🏠' },
  { path: '/models', label: 'Models', icon: '📦' },
  { path: '/providers', label: 'Providers', icon: '🔌' },
  { path: '/logs', label: 'Logs', icon: '📄' },
  { path: '/settings', label: 'Settings', icon: '⚙️' },
]

interface Props {
  open?: boolean
  onClose?: () => void
}

export default function Sidebar({ open = false, onClose }: Props) {
  const location = useLocation()

  return (
    <aside className={`sidebar${open ? ' open' : ''}`}>
      <div className="sidebar-logo">
        <div className="logo-icon">G</div>
        <div>
          <h1>LiteLLM</h1>
          <span>API Gateway</span>
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
            <span>{item.icon}</span>
            {item.label}
          </NavLink>
        ))}
      </nav>

      <div className="sidebar-footer">
        <div className="user-card">
          <div className="avatar">A</div>
          <div className="user-info">
            <div className="user-name">Admin</div>
            <div className="user-role">litellm-gateway</div>
          </div>
        </div>
      </div>
    </aside>
  )
}
