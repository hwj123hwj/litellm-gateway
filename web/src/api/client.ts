import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
  RoutesResponse,
} from './types'

// 获取后端地址（支持运行时配置）
function getBaseUrl(): string {
  // 优先使用 localStorage 中配置的地址
  const configured = localStorage.getItem('backend_url')
  if (configured) {
    const clean = configured.replace(/\/$/, '')
    return /\/admin\/?$/i.test(clean) ? clean : clean + '/admin'
  }
  // 默认使用相对路径（开发模式通过 Vite proxy）
  return '/admin'
}

async function fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const apiKey = localStorage.getItem('api_key') || ''
  const base = getBaseUrl()
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: {
      'Authorization': `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      ...(init.headers || {}),
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

export function getRoutes(): Promise<RoutesResponse> {
  return fetchJSON('/routes')
}

export function updateProvider(name: string, enabled: boolean): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}

export function resetProvider(name: string): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}/reset`, { method: 'POST' })
}

export function checkProvider(name: string): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}/health-check`, { method: 'POST' })
}

export function updateRoute(model: string, providers: string[]): Promise<unknown> {
  return fetchJSON(`/routes/${encodeURIComponent(model)}`, {
    method: 'PUT',
    body: JSON.stringify({ providers }),
  })
}

export function updateModel(
  model: string,
  capabilities: string[],
  inputModalities?: string[],
): Promise<unknown> {
  return fetchJSON(`/models/${encodeURIComponent(model)}`, {
    method: 'PUT',
    body: JSON.stringify({ capabilities, input_modalities: inputModalities }),
  })
}
