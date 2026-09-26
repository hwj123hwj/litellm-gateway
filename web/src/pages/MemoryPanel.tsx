import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Brain,
  CheckCircle,
  ChatCircleDots,
  FloppyDisk,
  Pause,
  Trash,
  Warning,
  XCircle,
} from '@phosphor-icons/react'
import {
  chatWithAssistant,
  confirmMemory,
  createMemory,
  deleteMemory,
  getMemories,
  retireMemory,
} from '../api'
import type { AssistantStreamEvent, MemoryEntry, MemoryStatus } from '../api/types'
import PageHeader from '../components/PageHeader'

// 记忆管家：EasyAgent 常驻助理对话 + 记忆治理（candidate → active / retired）。
// 治理边界与后端一致——助理只能提案 candidate，confirm/retire 只在这里由人操作。

const STATUS_FILTERS: { key: '' | MemoryStatus; label: string }[] = [
  { key: '', label: '全部' },
  { key: 'candidate', label: '待确认' },
  { key: 'active', label: '生效中' },
  { key: 'retired', label: '已退役' },
]

const STATUS_BADGE: Record<MemoryStatus, { cls: string; text: string }> = {
  candidate: { cls: 'degraded', text: '待确认' },
  active: { cls: 'ok', text: '生效中' },
  retired: { cls: 'error', text: '已退役' },
}

const SCOPE_LABEL: Record<string, string> = {
  global: '全局',
  client: '客户端',
  project: '项目',
}

function fmtTime(ts?: string) {
  if (!ts) return ''
  return new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function MemoryRow({
  item,
  onChanged,
}: {
  item: MemoryEntry
  onChanged: () => void
}) {
  const [busy, setBusy] = useState(false)
  const badge = STATUS_BADGE[item.status]

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      onChanged()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="settings-item settings-stack" style={{ minHeight: 0 }}>
      <div className="settings-heading">
        <div className="si-info" style={{ minWidth: 0 }}>
          <div className="si-desc" style={{ fontSize: 13, color: 'var(--text)' }}>{item.statement}</div>
          <div className="si-desc" style={{ marginTop: 4 }}>
            {SCOPE_LABEL[item.scope_type] || item.scope_type}
            {item.scope_key ? ` · ${item.scope_key}` : ''}
            {' · '}命中 {item.hit_count}
            {' · '}{fmtTime(item.created_at)}
            {' · '}{item.source}
          </div>
        </div>
        <span className={`status-badge ${badge.cls}`} style={{ marginLeft: 'auto', flexShrink: 0 }}>
          {badge.text}
        </span>
      </div>
      <div className="settings-control-row">
        {item.status === 'candidate' && (
          <button className="button button-primary" disabled={busy} onClick={() => act(() => confirmMemory(item.id))}>
            <CheckCircle size={14} weight="bold" aria-hidden="true" />确认生效
          </button>
        )}
        {(item.status === 'active' || item.status === 'candidate') && (
          <button className="button button-secondary" disabled={busy} onClick={() => act(() => retireMemory(item.id))}>
            <Pause size={14} weight="bold" aria-hidden="true" />退役
          </button>
        )}
        <button className="button button-ghost" disabled={busy} onClick={() => act(() => deleteMemory(item.id))}>
          <Trash size={14} weight="bold" aria-hidden="true" />删除
        </button>
      </div>
    </div>
  )
}

function AssistantCard() {
  const [messages, setMessages] = useState<{ role: 'user' | 'assistant'; text: string; tool?: string }[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const abortRef = useRef<{ abort: () => void } | null>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight })
  }, [messages])

  const send = () => {
    const message = input.trim()
    if (!message || streaming) return
    setInput('')
    setMessages((m) => [...m, { role: 'user', text: message }, { role: 'assistant', text: '' }])
    setStreaming(true)
    abortRef.current = chatWithAssistant(message, (ev: AssistantStreamEvent) => {
      setMessages((all) => {
        const next = [...all]
        const last = next[next.length - 1]
        if (ev.type === 'text_delta') {
          next[next.length - 1] = { ...last, text: last.text + (ev.content || '') }
        } else if (ev.type === 'tool_start') {
          next[next.length - 1] = { ...last, tool: ev.tool }
        } else if (ev.type === 'done') {
          if (ev.content) next[next.length - 1] = { ...last, text: ev.content }
        } else if (ev.type === 'error') {
          next[next.length - 1] = { ...last, text: last.text + `\n[错误] ${ev.content}` }
        }
        return next
      })
      if (ev.type === 'done' || ev.type === 'error') setStreaming(false)
    })
  }

  const stop = () => {
    abortRef.current?.abort()
    setStreaming(false)
  }

  return (
    <div className="card-panel">
      <div className="panel-header">
        <span className="panel-title">助理对话</span>
        <span className="si-desc">提案记忆 → 在下方治理区确认</span>
      </div>
      <div
        ref={listRef}
        style={{ display: 'flex', flexDirection: 'column', gap: 8, maxHeight: 320, overflowY: 'auto', marginBottom: 12 }}
      >
        {messages.length === 0 && (
          <div className="si-desc">让助理记录事实，例如「把这条记成候选记忆：……」</div>
        )}
        {messages.map((m, i) => (
          <div
            key={i}
            style={{
              alignSelf: m.role === 'user' ? 'flex-end' : 'flex-start',
              maxWidth: '85%',
              padding: '8px 11px',
              borderRadius: 10,
              fontSize: 12.5,
              lineHeight: 1.55,
              whiteSpace: 'pre-wrap',
              background: m.role === 'user' ? 'var(--accent-soft)' : 'var(--surface-muted)',
              border: '1px solid var(--line)',
            }}
          >
            {m.tool && (
              <div className="si-desc" style={{ marginBottom: 4 }}>
                <Brain size={12} weight="duotone" aria-hidden="true" /> 调用工具 {m.tool}
              </div>
            )}
            {m.text || (streaming && i === messages.length - 1 ? '…' : '')}
          </div>
        ))}
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <input
          className="text-input"
          style={{ flex: 1 }}
          placeholder={streaming ? '助理回复中…' : '给助理一条指令'}
          value={input}
          disabled={streaming}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && send()}
        />
        {streaming ? (
          <button className="button button-secondary" onClick={stop}>
            停止
          </button>
        ) : (
          <button className="button button-primary" onClick={send} disabled={!input.trim()}>
            <ChatCircleDots size={14} weight="bold" aria-hidden="true" />发送
          </button>
        )}
      </div>
    </div>
  )
}

export default function MemoryPanel() {
  const [memories, setMemories] = useState<MemoryEntry[] | null>(null)
  const [total, setTotal] = useState(0)
  const [filter, setFilter] = useState<'' | MemoryStatus>('')
  const [error, setError] = useState('')
  const [draft, setDraft] = useState('')
  const [draftScope, setDraftScope] = useState<'global' | 'client' | 'project'>('global')
  const [draftKey, setDraftKey] = useState('')
  const [saving, setSaving] = useState(false)

  const refresh = useCallback(() => {
    getMemories(filter)
      .then((r) => {
        setMemories(r.memories)
        setTotal(r.total)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [filter])

  useEffect(() => {
    refresh()
    const timer = setInterval(refresh, 15000)
    return () => clearInterval(timer)
  }, [refresh])

  const handleCreate = async () => {
    const statement = draft.trim()
    if (!statement) return
    setSaving(true)
    try {
      await createMemory({ scope_type: draftScope, scope_key: draftScope === 'project' ? draftKey.trim() : undefined, statement })
      setDraft('')
      setDraftKey('')
      refresh()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const pendingCount = memories?.filter((m) => m.status === 'candidate').length

  return (
    <>
      <PageHeader title="记忆管家" subtitle={`EasyAgent 常驻助理的记忆治理 · 共 ${total} 条${pendingCount ? ` · ${pendingCount} 条待确认` : ''}`} />
      {error && (
        <div className="error-banner" role="alert" style={{ marginBottom: 14 }}>
          <Warning size={18} weight="fill" aria-hidden="true" />{error}
        </div>
      )}
      <div style={{ display: 'grid', gridTemplateColumns: '1.6fr 1fr', gap: 16, alignItems: 'start' }}>
        <div className="card-panel">
          <div className="panel-header">
            <span className="panel-title">记忆条目</span>
            <div style={{ display: 'flex', gap: 6 }}>
              {STATUS_FILTERS.map((f) => (
                <button
                  key={f.key}
                  className={`button ${filter === f.key ? 'button-primary' : 'button-ghost'}`}
                  style={{ minHeight: 26, padding: '0 9px', fontSize: 11 }}
                  onClick={() => setFilter(f.key)}
                >
                  {f.label}
                </button>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {!memories && <div className="loading" role="status">正在读取记忆</div>}
            {memories?.length === 0 && (
              <div className="empty-state">
                <div className="empty-icon"><Brain size={36} weight="duotone" aria-hidden="true" /></div>
                <div className="empty-text">暂无记忆——在右侧让助理提案，或在下方手动添加</div>
              </div>
            )}
            {memories?.map((m) => (
              <MemoryRow key={m.id} item={m} onChanged={refresh} />
            ))}
          </div>
          <div className="panel-header" style={{ marginTop: 18, marginBottom: 10 }}>
            <span className="panel-title">手动添加</span>
            <span className="si-desc">人工添加直接生效，无需确认</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div style={{ display: 'flex', gap: 6 }}>
              {(['global', 'client', 'project'] as const).map((s) => (
                <button
                  key={s}
                  className={`button ${draftScope === s ? 'button-primary' : 'button-ghost'}`}
                  style={{ minHeight: 26, padding: '0 9px', fontSize: 11 }}
                  onClick={() => setDraftScope(s)}
                >
                  {SCOPE_LABEL[s]}
                </button>
              ))}
              {draftScope === 'project' && (
                <input
                  className="text-input"
                  style={{ flex: 1, minHeight: 28 }}
                  placeholder="host:owner/repo"
                  value={draftKey}
                  onChange={(e) => setDraftKey(e.target.value)}
                />
              )}
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <input
                className="text-input"
                style={{ flex: 1 }}
                placeholder="一条自包含的事实陈述"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
              />
              <button className="button button-primary" onClick={handleCreate} disabled={saving || !draft.trim()}>
                <FloppyDisk size={14} weight="bold" aria-hidden="true" />添加
              </button>
            </div>
          </div>
        </div>
        <AssistantCard />
      </div>
    </>
  )
}
