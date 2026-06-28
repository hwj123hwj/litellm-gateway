import { useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Sidebar from './components/Sidebar'
import Header from './components/Header'
import MobileTabBar from './components/MobileTabBar'
import Dashboard from './pages/Dashboard'
import Models from './pages/Models'
import Providers from './pages/Providers'
import Logs from './pages/Logs'
import Settings from './pages/Settings'
import { useStore } from './store'

// 检测是否在 Android 环境下
function isAndroid(): boolean {
  return /Android/i.test(navigator.userAgent)
}

// 检测是否已配置后端地址
function hasBackendUrl(): boolean {
  return !!localStorage.getItem('backend_url')
}

export default function App() {
  const {
    apiKey,
    setApiKey,
    backendUrl,
    health,
    fetchHealth,
  } = useStore()

  const [showSetup, setShowSetup] = useState(false)

  useEffect(() => {
    // Android 环境下，如果没有配置后端地址，显示配置引导
    if (isAndroid() && !hasBackendUrl()) {
      setShowSetup(true)
    }
  }, [])

  useEffect(() => {
    if (!apiKey || !backendUrl) return
    fetchHealth()
    const timer = setInterval(fetchHealth, 15000)
    return () => clearInterval(timer)
  }, [apiKey, backendUrl, fetchHealth])

  const status = health?.status || 'unknown'

  const handleSaveKey = (key: string) => {
    setApiKey(key)
    window.location.reload()
  }

  // 显示配置引导页面
  if (showSetup) {
    return (
      <div className="setup-page">
        <div className="setup-card">
          <div className="setup-icon">🚀</div>
          <h1>LiteLLM Admin</h1>
          <p>首次使用，请配置 Gateway 地址</p>
          <Settings />
          <button
            className="setup-button"
            onClick={() => {
              setShowSetup(false)
              window.location.reload()
            }}
          >
            开始使用
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="app-layout">
      <Sidebar onClose={() => {}} />

      <div className="shell">
        <Header status={status} onMenuClick={() => {}} />

        <main className="content">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/models" element={<Models />} />
            <Route path="/providers" element={<Providers />} />
            <Route path="/logs" element={<Logs />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>

        <MobileTabBar />
      </div>
    </div>
  )
}
