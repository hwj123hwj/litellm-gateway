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
  setBackendUrl,
  getBackendUrl,
} from '../api'

interface AppState {
  // Backend URL
  backendUrl: string
  setBackendUrl: (url: string) => void

  // Auth
  apiKey: string
  setApiKey: (key: string) => void

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
  backendUrl: getBackendUrl(),
  setBackendUrl: (url: string) => {
    setBackendUrl(url)
    set({ backendUrl: url })
  },

  // Auth
  apiKey: localStorage.getItem('api_key') || '',
  setApiKey: (key: string) => {
    localStorage.setItem('api_key', key)
    set({ apiKey: key })
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
