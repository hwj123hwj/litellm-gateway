import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
} from './types'

// 获取后端地址（支持运行时配置）
function getBaseUrl(): string {
  // 优先使用 localStorage 中配置的地址
  const configured = localStorage.getItem('backend_url')
  if (configured) {
    return configured.replace(/\/$/, '') + '/admin'
  }
  // 默认使用相对路径（开发模式通过 Vite proxy）
  return '/admin'
}

async function fetchJSON<T>(path: string): Promise<T> {
  const apiKey = localStorage.getItem('api_key') || ''
  const base = getBaseUrl()
  const res = await fetch(`${base}${path}`, {
    headers: {
      'Authorization': `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
    },
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

// 设置后端地址
export function setBackendUrl(url: string): void {
  localStorage.setItem('backend_url', url)
}

// 获取后端地址
export function getBackendUrl(): string {
  return localStorage.getItem('backend_url') || ''
}

export function getDashboard(): Promise<DashboardResponse> {
  return fetchJSON('/dashboard')
}

export function getProviders(): Promise<ProvidersResponse> {
  return fetchJSON('/providers')
}

export function getModels(): Promise<ModelsResponse> {
  return fetchJSON('/models')
}

export function getLogs(limit = 50): Promise<LogsResponse> {
  return fetchJSON(`/logs?limit=${limit}`)
}

export function getHealth(): Promise<HealthResponse> {
  return fetchJSON('/health')
}
