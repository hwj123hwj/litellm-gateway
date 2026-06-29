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

// ---------------------------------------------------------------------------
// 类型定义
// ---------------------------------------------------------------------------

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

// 使用 zustand 的原生 setter 类型，避免与 StoreSet 自定义类型冲突
type StoreSet = (
  partial:
    | Partial<AppState>
    | ((state: AppState) => Partial<AppState>),
) => void

// ---------------------------------------------------------------------------
// RequestGuard: 竞态保护 + 自动 abort 旧请求
// ---------------------------------------------------------------------------

/**
 * 每个数据域拥有独立的 RequestGuard。
 *
 * - generation 计数器：保证只有最后一次请求的响应才能写入 store
 * - currentController：新请求到来时 abort 上一个尚未完成的请求
 *
 * `K` 是该数据域在 AppState 中的键前缀（如 'dashboard'），
 * set 调用全部由 TypeScript 编译期检查，杜绝字符串拼写错误。
 */
class RequestGuard<
  K extends string,
  D,
> {
  private generation = 0
  private currentController: AbortController | null = null

  constructor(
    private set: StoreSet,
    private prefix: K,
  ) {}

  async run(fetcher: (signal: AbortSignal) => Promise<D>) {
    // 竞态保护：立即 abort 上一个尚未完成的请求
    if (this.currentController) {
      this.currentController.abort()
    }

    const controller = new AbortController()
    this.currentController = controller
    const gen = ++this.generation

    this.set({
      [`${this.prefix}Loading`]: true,
      [`${this.prefix}Error`]: null,
    } as Partial<AppState>)

    try {
      const data = await fetcher(controller.signal)
      if (gen === this.generation) {
        this.set({
          [this.prefix]: data,
          [`${this.prefix}Loading`]: false,
        } as Partial<AppState>)
      }
    } catch (e: any) {
      // 被取代的旧请求 (gen !== generation) 的错误被自然忽略
      if (gen === this.generation) {
        const err =
          e instanceof ApiError
            ? e
            : new ApiError(e?.message || '未知错误', 'NETWORK')
        this.set({
          [`${this.prefix}Error`]: err,
          [`${this.prefix}Loading`]: false,
        } as Partial<AppState>)
      }
    } finally {
      if (this.currentController === controller) {
        this.currentController = null
      }
    }
  }
}

// ---------------------------------------------------------------------------
// Store 创建
// ---------------------------------------------------------------------------

export const useStore = create<AppState>((set) => {
  const healthGuard = new RequestGuard<'health', HealthResponse>(set, 'health')
  const dashboardGuard = new RequestGuard<'dashboard', DashboardResponse>(
    set,
    'dashboard',
  )
  const providersGuard = new RequestGuard<'providers', ProvidersResponse>(
    set,
    'providers',
  )
  const modelsGuard = new RequestGuard<'models', ModelsResponse>(set, 'models')
  const logsGuard = new RequestGuard<'logs', LogsResponse>(set, 'logs')

  return {
    backendUrl: '',
    setBackendUrl: async (url) => {
      await saveBackendUrl(url)
      set({ backendUrl: url })
    },

    apiKey: '',
    setApiKey: async (key) => {
      await saveApiKey(key)
      set({ apiKey: key })
    },

    initialized: false,
    init: async () => {
      const [url, key] = await Promise.all([getBackendUrl(), getApiKey()])
      set({ backendUrl: url, apiKey: key, initialized: true })
    },

    health: null,
    healthLoading: false,
    healthError: null,
    fetchHealth: () => healthGuard.run((s) => getHealth(s)),

    dashboard: null,
    dashboardLoading: false,
    dashboardError: null,
    fetchDashboard: () => dashboardGuard.run((s) => getDashboard(s)),

    providers: null,
    providersLoading: false,
    providersError: null,
    fetchProviders: () => providersGuard.run((s) => getProviders(s)),

    models: null,
    modelsLoading: false,
    modelsError: null,
    fetchModels: () => modelsGuard.run((s) => getModels(s)),

    logs: null,
    logsLoading: false,
    logsError: null,
    fetchLogs: (limit = 100) => logsGuard.run((s) => getLogs(limit, s)),
  }
})
