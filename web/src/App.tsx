import { useState, useEffect, useCallback } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Sidebar from './components/Sidebar'
import Header from './components/Header'
import MobileTabBar from './components/MobileTabBar'
import Dashboard from './pages/Dashboard'
import Models from './pages/Models'
import Providers from './pages/Providers'
import Logs from './pages/Logs'
import Settings from './pages/Settings'
import type { HealthResponse } from './api'
import { getHealth } from './api'

export default function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [apiKey, setApiKey] = useState(() => localStorage.getItem('api_key') || '')

  const handleSaveKey = useCallback((key: string) => {
    setApiKey(key)
    localStorage.setItem('api_key', key)
  }, [])

  useEffect(() => {
    if (!apiKey) return
    const load = () => {
      getHealth()
        .then(setHealth)
        .catch(() => setHealth(null))
    }
    load()
    const timer = setInterval(load, 15000)
    return () => clearInterval(timer)
  }, [apiKey])

  const status = health?.status || 'unknown'

  return (
    <div className="app-layout">
      <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} />
      {sidebarOpen && <div className="overlay show" onClick={() => setSidebarOpen(false)} />}

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

        <Header
          status={status}
          onMenuClick={() => setSidebarOpen(true)}
        />

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
