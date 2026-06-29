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

/** 默认请求超时（毫秒）。超时后触发 AbortError，由 store RequestGuard 吞掉。 */
const DEFAULT_TIMEOUT_MS = 15_000

// 获取后端地址（支持运行时配置）
async function getBaseUrl(): Promise<string> {
  const configured = await AsyncStorage.getItem(STORAGE_KEYS.BACKEND_URL)
  if (configured) {
    return configured.replace(/\/$/, '') + '/admin'
  }
  // 默认地址（局域网开发）
  return 'http://10.0.2.2:4001/admin'
}

/**
 * 将外部 AbortSignal 与超时 signal 合并。
 * 任一触发都会 abort 请求。
 * 通过 addEventListener 实现时，会在 fetch 完成时通过返回的 cleanup 移除监听。
 */
function mergeSignals(
  external?: AbortSignal,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): { signal: AbortSignal; cleanup: () => void } {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)

  // 用于移除外部 signal 上的事件监听器，避免内存泄漏
  let onExternalAbort: (() => void) | null = null

  if (external) {
    if (external.aborted) {
      controller.abort()
    } else {
      onExternalAbort = () => controller.abort()
      external.addEventListener('abort', onExternalAbort, { once: true })
    }
  }

  return {
    signal: controller.signal,
    cleanup: () => {
      clearTimeout(timer)
      if (onExternalAbort && external) {
        external.removeEventListener('abort', onExternalAbort)
      }
    },
  }
}

/**
 * 统一的 JSON 请求方法。
 * - 支持 AbortSignal（由调用方传入，通常来自 store 的 RequestGuard）
 * - 默认 15s 超时，超时后 abort
 * - 对常见 HTTP 状态码做结构化错误分类，方便 UI 层给出精确提示
 */
async function fetchJSON<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  const apiKey = (await AsyncStorage.getItem(STORAGE_KEYS.API_KEY)) || ''
  const base = await getBaseUrl()
  const { signal: mergedSignal, cleanup } = mergeSignals(signal)

  let res: Response
  try {
    res = await fetch(`${base}${path}`, {
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'Content-Type': 'application/json',
      },
      signal: mergedSignal,
    })
  } catch (e: any) {
    cleanup()
    // 网络层错误（DNS、连接拒绝、超时 abort 等）
    if (e?.name === 'AbortError') {
      throw new ApiError('请求超时，请检查网络连接', 'TIMEOUT')
    }
    throw new ApiError('网络连接失败，请检查后端地址', 'NETWORK')
  }
  cleanup()

  if (!res.ok) {
    // 结构化 HTTP 错误
    if (res.status === 401 || res.status === 403) {
      throw new ApiError('认证失败，请检查 API Key', 'AUTH')
    }
    if (res.status >= 500) {
      throw new ApiError(`服务器错误 (${res.status})`, 'SERVER')
    }
    throw new ApiError(`请求失败: ${res.status} ${res.statusText}`, 'CLIENT')
  }
  return res.json()
}

/** 结构化 API 错误，带可分类的 code 字段，方便 UI 精确提示 */
export class ApiError extends Error {
  code: 'TIMEOUT' | 'NETWORK' | 'AUTH' | 'SERVER' | 'CLIENT'
  constructor(
    message: string,
    code: ApiError['code'],
  ) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
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

export function getDashboard(signal?: AbortSignal): Promise<DashboardResponse> {
  return fetchJSON('/dashboard', signal)
}

export function getProviders(
  signal?: AbortSignal,
): Promise<ProvidersResponse> {
  return fetchJSON('/providers', signal)
}

export function getModels(signal?: AbortSignal): Promise<ModelsResponse> {
  return fetchJSON('/models', signal)
}

export function getLogs(
  limit = 50,
  signal?: AbortSignal,
): Promise<LogsResponse> {
  return fetchJSON(`/logs?limit=${limit}`, signal)
}

export function getHealth(signal?: AbortSignal): Promise<HealthResponse> {
  return fetchJSON('/health', signal)
}
