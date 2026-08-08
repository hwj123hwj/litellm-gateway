export interface DashboardSummary {
  today_requests: number
  success_rate: number
  active_models: number
  avg_latency_ms: number
  uptime: string
}

export interface ProviderInfo {
  name: string
  status: 'online' | 'degraded' | 'offline' | 'unknown'
  state?: 'closed' | 'open' | 'half_open'
  enabled?: boolean
  consecutive_failures?: number
  next_retry_at?: string
  requests: number
  successes?: number
  errors?: number
  avg_latency: number
  last_check?: string
}

export interface ModelInfo {
  model: string
  provider?: string
  status: 'online' | 'degraded' | 'offline' | 'idle'
  requests: number
  total_tokens: number
  avg_latency: number
  successes?: number
  errors?: number
  capabilities?: string[]
  input_modalities?: string[]
  providers?: string[]
}

export interface LogEntry {
  timestamp: string
  request_id?: string
  method: string
  path: string
  model: string
  provider: string
  status_code: number
  latency_ms: number
  input_tokens: number
  output_tokens: number
  is_stream: boolean
  error?: string
  provider_attempts?: ProviderAttempt[]
}

export interface ProviderAttempt {
  provider: string
  status: 'success' | 'error' | 'skipped' | string
  status_code?: number
  latency_ms: number
  error?: string
}

export interface DashboardResponse {
  summary: DashboardSummary
  providers: ProviderInfo[]
  models: ModelInfo[]
}

export interface ProvidersResponse {
  providers: ProviderInfo[]
  total: number
}

export interface ModelsResponse {
  models: ModelInfo[]
  total: number
}

export interface LogsResponse {
  logs: LogEntry[]
  total: number
}

export interface HealthResponse {
  status: string
  uptime: string
  today_requests: number
  success_rate: number
  active_models: number
  avg_latency_ms: number
  providers_online: number
  providers_total: number
  providers: { provider: string; status: string }[]
}
