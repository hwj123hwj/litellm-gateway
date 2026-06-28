import AsyncStorage from '@react-native-async-storage/async-storage'
import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
} from './types'

const STORAGE_KEYS = {
  BACKEND_URL: 'backend_url',
  API_KEY: 'api_key',
}

// 获取后端地址（支持运行时配置）
async function getBaseUrl(): Promise<string> {
  const configured = await AsyncStorage.getItem(STORAGE_KEYS.BACKEND_URL)
  if (configured) {
    return configured.replace(/\/$/, '') + '/admin'
  }
  // 默认地址（局域网开发）
  return 'http://10.0.2.2:4001/admin'
}

async function fetchJSON<T>(path: string): Promise<T> {
  const apiKey = (await AsyncStorage.getItem(STORAGE_KEYS.API_KEY)) || ''
  const base = await getBaseUrl()
  const res = await fetch(`${base}${path}`, {
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
    },
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

// 设置后端地址
export async function setBackendUrl(url: string): Promise<void> {
  await AsyncStorage.setItem(STORAGE_KEYS.BACKEND_URL, url)
}

// 获取后端地址
export async function getBackendUrl(): Promise<string> {
  return (await AsyncStorage.getItem(STORAGE_KEYS.BACKEND_URL)) || ''
}

// 设置 API Key
export async function setApiKey(key: string): Promise<void> {
  await AsyncStorage.setItem(STORAGE_KEYS.API_KEY, key)
}

// 获取 API Key
export async function getApiKey(): Promise<string> {
  return (await AsyncStorage.getItem(STORAGE_KEYS.API_KEY)) || ''
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
