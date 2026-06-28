import { NavLink } from 'react-router-dom'

const TABS = [
  { path: '/', label: '首页', icon: '🏠' },
  { path: '/models', label: '模型', icon: '📦' },
  { path: '/logs', label: '日志', icon: '📄' },
  { path: '/settings', label: '设置', icon: '⚙️' },
]

export default function MobileTabBar() {
  return (
    <nav className="mobile-tabbar">
      {TABS.map((tab) => (
        <NavLink
          key={tab.path}
          to={tab.path}
          className={({ isActive }) => `tab-item${isActive ? ' active' : ''}`}
          end={tab.path === '/'}
        >
          <span className="tab-icon">{tab.icon}</span>
          <span>{tab.label}</span>
        </NavLink>
      ))}
    </nav>
  )
}
