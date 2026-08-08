import { create } from 'zustand'
import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
  RoutesResponse,
} from '../api'
import {
  getDashboard,
  getProviders,
  getModels,
  getLogs,
  getHealth,
  getRoutes,
  updateProvider as apiUpdateProvider,
  resetProvider as apiResetProvider,
  checkProvider as apiCheckProvider,
  updateRoute as apiUpdateRoute,
  updateModel as apiUpdateModel,
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

  // Failover routes and controls
  routes: RoutesResponse | null
  routesLoading: boolean
  routesError: string | null
  fetchRoutes: () => Promise<void>
  updateProvider: (name: string, enabled: boolean) => Promise<void>
  resetProvider: (name: string) => Promise<void>
  checkProvider: (name: string) => Promise<void>
  updateRoute: (model: string, providers: string[]) => Promise<void>
  updateModel: (model: string, capabilities: string[], inputModalities?: string[]) => Promise<void>

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

  routes: null,
  routesLoading: false,
  routesError: null,
  fetchRoutes: async () => {
    set({ routesLoading: true, routesError: null })
    try {
      const routes = await getRoutes()
      set({ routes, routesLoading: false })
    } catch (e: any) {
      set({ routesError: e.message, routesLoading: false })
    }
  },
  updateProvider: async (name, enabled) => {
    await apiUpdateProvider(name, enabled)
    await get().fetchProviders()
    await get().fetchRoutes()
  },
  resetProvider: async (name) => {
    await apiResetProvider(name)
    await get().fetchProviders()
    await get().fetchRoutes()
  },
  checkProvider: async (name) => {
    await apiCheckProvider(name)
    await get().fetchProviders()
    await get().fetchRoutes()
  },
  updateRoute: async (model, providers) => {
    await apiUpdateRoute(model, providers)
    await get().fetchRoutes()
  },
  updateModel: async (model, capabilities, inputModalities) => {
    await apiUpdateModel(model, capabilities, inputModalities)
    await get().fetchModels()
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
