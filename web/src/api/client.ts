import type {
  DashboardResponse,
  ProvidersResponse,
  ModelsResponse,
  LogsResponse,
  HealthResponse,
  RoutesResponse,
  PiConfigResponse,
  MemoriesResponse,
  MemoryEntry,
  AssistantStreamEvent,
  AssistantPromptResponse,
  AssistantFeedbackEntry,
} from './types'

// 获取后端地址（支持运行时配置）
function getBaseUrl(): string {
  // 优先使用 localStorage 中配置的地址
  const configured = localStorage.getItem('backend_url')
  if (configured) {
    const clean = configured.replace(/\/$/, '')
    return /\/admin\/?$/i.test(clean) ? clean : clean + '/admin'
  }
  // 默认使用相对路径（开发模式通过 Vite proxy）
  return '/admin'
}

async function fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const apiKey = localStorage.getItem('api_key') || ''
  const base = getBaseUrl()
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: {
      'Authorization': `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

// 设置后端地址
export function setBackendUrl(url: string): void {
  localStorage.setItem('backend_url', url)
}

// 获取后端地址
export function getBackendUrl(): string {
  return localStorage.getItem('backend_url') || ''
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

export function getRoutes(): Promise<RoutesResponse> {
  return fetchJSON('/routes')
}

export function updateProvider(name: string, enabled: boolean): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}

export function resetProvider(name: string): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}/reset`, { method: 'POST' })
}

export function checkProvider(name: string): Promise<unknown> {
  return fetchJSON(`/providers/${encodeURIComponent(name)}/health-check`, { method: 'POST' })
}

export function updateRoute(model: string, providers: string[]): Promise<unknown> {
  return fetchJSON(`/routes/${encodeURIComponent(model)}`, {
    method: 'PUT',
    body: JSON.stringify({ providers }),
  })
}

export function updateModel(
  model: string,
  capabilities: string[],
  inputModalities?: string[],
): Promise<unknown> {
  return fetchJSON(`/models/${encodeURIComponent(model)}`, {
    method: 'PUT',
    body: JSON.stringify({ capabilities, input_modalities: inputModalities }),
  })
}

export function getPiConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/pi')
}

export function syncPiConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/pi/sync', { method: 'POST' })
}

export function getZCodeConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/zcode')
}

export function syncZCodeConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/zcode/sync', { method: 'POST' })
}

export function getHarnessConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/harness')
}

export function syncHarnessConfig(): Promise<PiConfigResponse> {
  return fetchJSON('/harness/sync', { method: 'POST' })
}

// ── 记忆层 ──

export function getMemories(status = '', limit = 200): Promise<MemoriesResponse> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (status) params.set('status', status)
  return fetchJSON(`/memories?${params}`)
}

export function confirmMemory(id: number): Promise<unknown> {
  return fetchJSON(`/memories/${id}/confirm`, { method: 'POST' })
}

export function retireMemory(id: number): Promise<unknown> {
  return fetchJSON(`/memories/${id}/retire`, { method: 'POST' })
}

export function deleteMemory(id: number): Promise<unknown> {
  return fetchJSON(`/memories/${id}`, { method: 'DELETE' })
}

export function createMemory(body: {
  scope_type: string
  scope_key?: string
  statement: string
  confidence?: number
}): Promise<{ memory: MemoryEntry }> {
  return fetchJSON('/memories', { method: 'POST', body: JSON.stringify(body) })
}

// 助理对话（SSE 流式）。onEvent 逐帧回调；返回的 abort 可中断。
export function chatWithAssistant(
  message: string,
  onEvent: (ev: AssistantStreamEvent) => void,
): { abort: () => void } {
  const controller = new AbortController()
  const apiKey = localStorage.getItem('api_key') || ''
  const base = getBaseUrl()
  fetch(`${base}/assistant/chat`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${apiKey}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ message }),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (!res.ok || !res.body) {
        onEvent({ type: 'error', content: `API error: ${res.status}` })
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const frames = buffer.split('\n\n')
        buffer = frames.pop() || ''
        for (const frame of frames) {
          const line = frame.split('\n').find((l) => l.startsWith('data: '))
          if (!line) continue
          try {
            onEvent(JSON.parse(line.slice(6)) as AssistantStreamEvent)
          } catch {
            // 忽略不完整帧
          }
        }
      }
    })
    .catch((err: unknown) => {
      if ((err as Error).name !== 'AbortError') {
        onEvent({ type: 'error', content: (err as Error).message })
      }
    })
  return { abort: () => controller.abort() }
}

// ── 助理人设与反馈 ──

export function getAssistantPrompt(): Promise<AssistantPromptResponse> {
  return fetchJSON('/assistant/prompt')
}

export function setAssistantPrompt(prompt: string): Promise<AssistantPromptResponse> {
  return fetchJSON('/assistant/prompt', { method: 'PUT', body: JSON.stringify({ prompt }) })
}

export function addAssistantFeedback(rating: 'up' | 'down', reply: string, note = ''): Promise<unknown> {
  return fetchJSON('/assistant/feedback', {
    method: 'POST',
    body: JSON.stringify({ rating, reply, note }),
  })
}

export function listAssistantFeedback(limit = 50): Promise<{ feedback: AssistantFeedbackEntry[]; total: number }> {
  return fetchJSON(`/assistant/feedback?limit=${limit}`)
}
