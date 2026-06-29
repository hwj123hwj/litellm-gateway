import { create } from 'zustand'
import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
} from '../api'
import {
  getDashboard,
  getProviders,
  getModels,
  getLogs,
  getHealth,
  setBackendUrl as saveBackendUrl,
  getBackendUrl,
  setApiKey as saveApiKey,
  getApiKey,
  ApiError,
} from '../api'

/**
 * 竞态保护：每个数据域维护独立的 generation 计数器，
 * 保证只有最后一次请求的响应才能写入 store。
 * 保留完整的 ApiError 对象（而非仅 message），以便 UI 根据 code 做差异化处理。
 */
class RequestGuard<T> {
  private generation = 0
  /** 当前活跃请求的 AbortController，新的请求发起时会 abort 上一个 */
  private currentController: AbortController | null = null

  constructor(
    private set: (partial: Partial<Record<string, unknown>>) => void,
    private keys: { data: string; loading: string; error: string },
  ) {}

  async run(fetcher: (signal: AbortSignal) => Promise<T>) {
    // 竞态保护核心：新请求到来时，立即 abort 上一个尚未完成的请求
    // 这样旧请求的 fetch 网络层会被真正取消，而非空跑浪费带宽
    if (this.currentController) {
      this.currentController.abort()
    }

    const controller = new AbortController()
    this.currentController = controller
    const gen = ++this.generation
    this.set({
      [this.keys.loading]: true,
      [this.keys.error]: null,
    })

    try {
      const data = await fetcher(controller.signal)
      // 仅当本次请求仍是最新一次时才写入
      if (gen === this.generation) {
        this.set({ [this.keys.data]: data, [this.keys.loading]: false })
      }
    } catch (e: any) {
      // fetchJSON 已将所有 AbortError 包装为 ApiError('TIMEOUT')，
      // 所以这里不会收到原始 AbortError。
      // 对于被新请求取代的旧请求，gen !== this.generation，
      // 其错误被自然忽略，不会写入 store。
      if (gen === this.generation) {
        const err =
          e instanceof ApiError
            ? e
            : new ApiError(e?.message || '未知错误', 'NETWORK')
        this.set({ [this.keys.error]: err, [this.keys.loading]: false })
      }
    } finally {
      // 清理 currentController 引用，避免对一个已完成的请求反复 abort
      if (this.currentController === controller) {
        this.currentController = null
      }
    }
  }
}

interface AppState {
  // Backend URL
  backendUrl: string
  setBackendUrl: (url: string) => Promise<void>

  // Auth
  apiKey: string
  setApiKey: (key: string) => Promise<void>

  // Initialization
  initialized: boolean
  init: () => Promise<void>

  // Health
  health: HealthResponse | null
  healthLoading: boolean
  healthError: ApiError | null
  fetchHealth: () => Promise<void>

  // Dashboard
  dashboard: DashboardResponse | null
  dashboardLoading: boolean
  dashboardError: ApiError | null
  fetchDashboard: () => Promise<void>

  // Providers
  providers: ProvidersResponse | null
  providersLoading: boolean
  providersError: ApiError | null
  fetchProviders: () => Promise<void>

  // Models
  models: ModelsResponse | null
  modelsLoading: boolean
  modelsError: ApiError | null
  fetchModels: () => Promise<void>

  // Logs
  logs: LogsResponse | null
  logsLoading: boolean
  logsError: ApiError | null
  fetchLogs: (limit?: number) => Promise<void>
}

export const useStore = create<AppState>((set) => {
  // 每个数据域独立的 RequestGuard，互不干扰
  const healthGuard = new RequestGuard<HealthResponse>(set, {
    data: 'health',
    loading: 'healthLoading',
    error: 'healthError',
  })
  const dashboardGuard = new RequestGuard<DashboardResponse>(set, {
    data: 'dashboard',
    loading: 'dashboardLoading',
    error: 'dashboardError',
  })
  const providersGuard = new RequestGuard<ProvidersResponse>(set, {
    data: 'providers',
    loading: 'providersLoading',
    error: 'providersError',
  })
  const modelsGuard = new RequestGuard<ModelsResponse>(set, {
    data: 'models',
    loading: 'modelsLoading',
    error: 'modelsError',
  })
  const logsGuard = new RequestGuard<LogsResponse>(set, {
    data: 'logs',
    loading: 'logsLoading',
    error: 'logsError',
  })

  return {
    // Backend URL
    backendUrl: '',
    setBackendUrl: async (url: string) => {
      await saveBackendUrl(url)
      set({ backendUrl: url })
    },

    // Auth
    apiKey: '',
    setApiKey: async (key: string) => {
      await saveApiKey(key)
      set({ apiKey: key })
    },

    // Initialization
    initialized: false,
    init: async () => {
      const [url, key] = await Promise.all([getBackendUrl(), getApiKey()])
      set({ backendUrl: url, apiKey: key, initialized: true })
    },

    // Health
    health: null,
    healthLoading: false,
    healthError: null,
    fetchHealth: () => healthGuard.run((s) => getHealth(s)),

    // Dashboard
    dashboard: null,
    dashboardLoading: false,
    dashboardError: null,
    fetchDashboard: () => dashboardGuard.run((s) => getDashboard(s)),

    // Providers
    providers: null,
    providersLoading: false,
    providersError: null,
    fetchProviders: () => providersGuard.run((s) => getProviders(s)),

    // Models
    models: null,
    modelsLoading: false,
    modelsError: null,
    fetchModels: () => modelsGuard.run((s) => getModels(s)),

    // Logs
    logs: null,
    logsLoading: false,
    logsError: null,
    fetchLogs: (limit = 100) => logsGuard.run((s) => getLogs(limit, s)),
  }
})
