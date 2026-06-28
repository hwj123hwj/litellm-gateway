import { useCallback } from 'react'
import { getLogs } from '../api'
import { useFetch } from '../hooks'
import PageHeader from '../components/PageHeader'

function fmtTime(ts: string) {
  const d = new Date(ts)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return Math.floor(diff / 60_000) + ' 分钟前'
  if (diff < 86_400_000) return Math.floor(diff / 3_600_000) + ' 小时前'
  return d.toLocaleDateString('zh-CN')
}

function fmtLatency(ms: number) {
  return ms < 1000 ? ms.toFixed(0) + 'ms' : (ms / 1000).toFixed(1) + 's'
}

export default function Logs() {
  const fetcher = useCallback(() => getLogs(100), [])
  const { data, error, loading } = useFetch(fetcher, { refreshInterval: 10000 })

  if (loading) return <div className="loading">加载中...</div>
  if (error) return <div className="error-banner">⚠ {error}</div>
  if (!data) return null

  return (
    <>
      <PageHeader title="活动日志" subtitle={`最近 ${data.total} 条请求记录`} />
      <div className="logs-list">
        {data.logs.length === 0 && (
          <div className="empty-state">
            <div className="empty-icon">📄</div>
            <div className="empty-text">暂无日志，等待第一个请求...</div>
          </div>
        )}
        {data.logs.map((log, i) => {
          const isSuccess = log.status_code >= 200 && log.status_code < 400
          const statusClass = log.error ? 'error' : isSuccess ? 'success' : 'error'
          const statusText = log.error ? '错误' : isSuccess ? '成功' : `${log.status_code}`

          return (
            <div key={i} className="log-entry">
              <div className="log-top">
                <span className="log-model">
                  {log.provider ? `${log.provider} · ` : ''}{log.model || log.path}
                </span>
                <span className={`log-status ${statusClass}`}>{statusText}</span>
              </div>
              <div className="log-detail">
                {log.method} {log.path}
                {log.latency_ms > 0 && ` · ${fmtLatency(log.latency_ms)}`}
                {log.input_tokens > 0 && ` · ${log.input_tokens + log.output_tokens} tokens`}
                {log.is_stream && ' · 流式'}
                {log.error && ` · ${log.error}`}
              </div>
              <div className="log-time">{fmtTime(log.timestamp)}</div>
            </div>
          )
        })}
      </div>
    </>
  )
}
