import { useEffect } from 'react'
import { Broadcast, CheckCircle, Clock, FileText, Warning, XCircle } from '@phosphor-icons/react'
import { useStore } from '../store'
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
  const { logs, logsLoading, logsError, fetchLogs } = useStore()

  useEffect(() => {
    fetchLogs(100)
    const timer = setInterval(() => fetchLogs(100), 10000)
    return () => clearInterval(timer)
  }, [fetchLogs])

  if (logsLoading && !logs) return <div className="loading" role="status">正在读取请求记录</div>
  if (logsError) return <div className="error-banner" role="alert"><Warning size={18} weight="fill" aria-hidden="true" />{logsError}</div>
  if (!logs) return null

  return (
    <>
      <PageHeader title="活动日志" subtitle={`最近 ${logs.total} 条业务请求记录`} />
      <div className="logs-list">
        {logs.logs.length === 0 && (
          <div className="empty-state">
            <div className="empty-icon"><FileText size={36} weight="duotone" aria-hidden="true" /></div>
            <div className="empty-text">暂无业务请求，等待第一个调用</div>
          </div>
        )}
        {logs.logs.map((log, i) => {
          const isSuccess = log.status_code >= 200 && log.status_code < 400
          const statusClass = log.error ? 'error' : isSuccess ? 'success' : 'error'
          const statusText = log.error ? '错误' : isSuccess ? '成功' : `${log.status_code}`
          const StatusIcon = log.error ? XCircle : isSuccess ? CheckCircle : Warning

          return (
            <article key={i} className="log-entry">
              <div className="log-top">
                <span className="log-model">
                  {log.provider ? `${log.provider} / ` : ''}{log.model || log.path}
                </span>
                <span className={`log-status ${statusClass}`}><StatusIcon size={13} weight="fill" aria-hidden="true" />{statusText}</span>
              </div>
              <div className="log-detail">
                <span>{log.method} {log.path}</span>
                {log.latency_ms > 0 && <span><Clock size={13} aria-hidden="true" />{fmtLatency(log.latency_ms)}</span>}
                {log.input_tokens > 0 && <span>{log.input_tokens + log.output_tokens} tokens</span>}
                {log.is_stream && <span><Broadcast size={13} aria-hidden="true" />流式</span>}
                {log.request_id && <span className="log-request-id">{log.request_id}</span>}
              </div>
              {log.error && <div className="log-error"><Warning size={13} weight="fill" aria-hidden="true" />{log.error}</div>}
              <div className="log-time">{fmtTime(log.timestamp)}</div>
            </article>
          )
        })}
      </div>
    </>
  )
}
