import { useEffect } from 'react'
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

export default function App() {
  const {
    apiKey,
    setApiKey,
    health,
    fetchHealth,
  } = useStore()

  useEffect(() => {
    if (!apiKey) return
    fetchHealth()
    const timer = setInterval(fetchHealth, 15000)
    return () => clearInterval(timer)
  }, [apiKey, fetchHealth])

  const status = health?.status || 'unknown'

  const handleSaveKey = (key: string) => {
    setApiKey(key)
    window.location.reload()
  }

  return (
    <div className="app-layout">
      <Sidebar onClose={() => {}} />

      <div className="shell">
        <div className="api-key-bar">
          <input
            type="password"
            placeholder="输入 API Key（LITELLM_MASTER_KEY）"
            value={apiKey}
            onChange={(e) => handleSaveKey(e.target.value)}
          />
          <button onClick={() => window.location.reload()}>连接</button>
        </div>

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
