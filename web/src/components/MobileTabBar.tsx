import { NavLink } from 'react-router-dom'
import { ChartLineUp, Cube, FileText, Gear } from '@phosphor-icons/react'

const TABS = [
  { path: '/', label: '首页', icon: ChartLineUp },
  { path: '/models', label: '模型', icon: Cube },
  { path: '/logs', label: '日志', icon: FileText },
  { path: '/settings', label: '设置', icon: Gear },
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
          <tab.icon className="tab-icon" size={19} weight="regular" aria-hidden="true" />
          <span>{tab.label}</span>
        </NavLink>
      ))}
    </nav>
  )
}
