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
} from '../api'

interface AppState {
  // Backend URL
  backendUrl: string
  initBackendUrl: () => Promise<void>
  setBackendUrl: (url: string) => Promise<void>

  // Auth
  apiKey: string
  initApiKey: () => Promise<void>
  setApiKey: (key: string) => Promise<void>

  // Initialization
  initialized: boolean
  init: () => Promise<void>

  // Health
  health: HealthResponse | null
  healthLoading: boolean
  healthError: string | null
  fetchHealth: () => Promise<void>

  // Dashboard
  dashboard: DashboardResponse | null
  dashboardLoading: boolean
  dashboardError: string | null
  fetchDashboard: () => Promise<void>

  // Providers
  providers: ProvidersResponse | null
  providersLoading: boolean
  providersError: string | null
  fetchProviders: () => Promise<void>

  // Models
  models: ModelsResponse | null
  modelsLoading: boolean
  modelsError: string | null
  fetchModels: () => Promise<void>

  // Logs
  logs: LogsResponse | null
  logsLoading: boolean
  logsError: string | null
  fetchLogs: (limit?: number) => Promise<void>
}

export const useStore = create<AppState>((set, get) => ({
  // Backend URL
  backendUrl: '',
  initBackendUrl: async () => {
    const url = await getBackendUrl()
    set({ backendUrl: url })
  },
  setBackendUrl: async (url: string) => {
    await saveBackendUrl(url)
    set({ backendUrl: url })
  },

  // Auth
  apiKey: '',
  initApiKey: async () => {
    const key = await getApiKey()
    set({ apiKey: key })
  },
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
  fetchHealth: async () => {
    set({ healthLoading: true, healthError: null })
    try {
      const health = await getHealth()
      set({ health, healthLoading: false })
    } catch (e: any) {
      set({ healthError: e.message, healthLoading: false })
    }
  },

  // Dashboard
  dashboard: null,
  dashboardLoading: false,
  dashboardError: null,
  fetchDashboard: async () => {
    set({ dashboardLoading: true, dashboardError: null })
    try {
      const dashboard = await getDashboard()
      set({ dashboard, dashboardLoading: false })
    } catch (e: any) {
      set({ dashboardError: e.message, dashboardLoading: false })
    }
  },

  // Providers
  providers: null,
  providersLoading: false,
  providersError: null,
  fetchProviders: async () => {
    set({ providersLoading: true, providersError: null })
    try {
      const providers = await getProviders()
      set({ providers, providersLoading: false })
    } catch (e: any) {
      set({ providersError: e.message, providersLoading: false })
    }
  },

  // Models
  models: null,
  modelsLoading: false,
  modelsError: null,
  fetchModels: async () => {
    set({ modelsLoading: true, modelsError: null })
    try {
      const models = await getModels()
      set({ models, modelsLoading: false })
    } catch (e: any) {
      set({ modelsError: e.message, modelsLoading: false })
    }
  },

  // Logs
  logs: null,
  logsLoading: false,
  logsError: null,
  fetchLogs: async (limit = 100) => {
    set({ logsLoading: true, logsError: null })
    try {
      const logs = await getLogs(limit)
      set({ logs, logsLoading: false })
    } catch (e: any) {
      set({ logsError: e.message, logsLoading: false })
    }
  },
}))
