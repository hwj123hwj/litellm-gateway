export interface DashboardSummary {
  today_requests: number
  success_rate: number
  active_models: number
  avg_latency_ms: number
  uptime: string
  cache_read_input_tokens?: number
  cache_creation_input_tokens?: number
  cache_input_tokens?: number
  cache_usage_requests?: number
  cache_hit_rate?: number | null
}

export interface ProviderInfo {
  name: string
  status: 'online' | 'degraded' | 'offline' | 'unknown'
  state?: 'closed' | 'open' | 'half_open'
  enabled?: boolean
  consecutive_failures?: number
  total_failures?: number
  total_successes?: number
  next_retry_at?: string
  // 最近一次上游探测的结论。has_probe 为 false 表示从未探测过，
  // 此时的 status 只反映熔断器，不代表上游可用性。
  has_probe?: boolean
  probe_status?: 'online' | 'degraded' | 'offline' | 'unknown'
  probe_detail?: string
  last_probe_at?: string
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
  cache_read_input_tokens?: number
  cache_creation_input_tokens?: number
  cache_input_tokens?: number
  cache_usage_requests?: number
  cache_hit_rate?: number | null
}

export interface RouteProviderInfo {
  name: string
  enabled: boolean
  state: 'closed' | 'open' | 'half_open'
  status: 'online' | 'degraded' | 'offline' | 'unknown'
}

export interface RouteInfo {
  model: string
  providers: RouteProviderInfo[]
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
  cache_read_input_tokens?: number
  cache_creation_input_tokens?: number
  cache_input_tokens?: number
  cache_usage_known?: boolean
  cache_hit_rate?: number | null
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

export interface RoutesResponse {
  routes: RouteInfo[]
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

export interface PiModelEntry {
  id: string
  name: string
}

export interface PiConfigResponse {
  path: string
  file_exists: boolean
  in_sync: boolean
  desired: PiModelEntry[]
  desired_ids: string[]
  current_ids: string[]
  missing_ids: string[]
  stale_ids: string[]
  missing_entries?: string[]
  skipped_entries?: string[]
  synced?: boolean
  error?: string
}

// ── 技能面板（custom-skills 技能市场）──

export interface SkillEntry {
  id: string
  displayName?: string
  description?: string
  emoji?: string
  tags?: string[]
  author?: string
  sourcePath?: string
  githubUrl?: string
  lastUpdated?: string
  upstream?: string
}

export interface SkillInstallRow extends SkillEntry {
  enabled: boolean
  installed: Record<string, boolean>
}

export interface SkillsSyncTargetReport {
  target: string
  linked: string[] | null
  current: string[] | null
  removed: string[] | null
  skipped: string[] | null
  errors: string[] | null
}

export interface SkillsStatusResponse {
  project?: string
  local_skills?: { id: string; path: string; source: string }[]
  configured: boolean
  hint?: string
  repo?: string
  registry_path?: string
  manifest_path?: string
  manifest_exists?: boolean
  targets?: string[]
  enabled?: string[]
  in_sync?: boolean
  stale_links?: string[]
  skills?: SkillInstallRow[]
  sync?: { targets: SkillsSyncTargetReport[] }
  error?: string
}

// ── 记忆层（EasyAgent 常驻助理 + 治理）──

export type MemoryStatus = 'candidate' | 'active' | 'retired'
export type MemoryScopeType = 'global' | 'client' | 'project'

export interface MemoryEntry {
  id: number
  scope_type: MemoryScopeType
  scope_key: string
  statement: string
  status: MemoryStatus
  source: string
  confidence: number
  hit_count: number
  last_hit_at?: string
  created_at: string
  updated_at: string
}

export interface MemoriesResponse {
  memories: MemoryEntry[]
  total: number
  limit: number
  offset: number
}

export interface AssistantStreamEvent {
  type: 'text_delta' | 'tool_start' | 'tool_end' | 'done' | 'error'
  content?: string
  tool?: string
}

export interface AssistantPromptResponse {
  prompt: string
  custom: string
  default: string
}

export interface AssistantFeedbackEntry {
  id: number
  rating: 'up' | 'down'
  reply_excerpt: string
  note: string
  created_at: string
}
