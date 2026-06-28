import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
} from './types'

const BASE = '/admin'

async function fetchJSON<T>(path: string): Promise<T> {
  const apiKey = localStorage.getItem('api_key') || ''
  const res = await fetch(`${BASE}${path}`, {
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
