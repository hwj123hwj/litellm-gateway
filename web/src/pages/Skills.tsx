import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  CheckCircle,
  Lightning,
  MagnifyingGlass,
  PlugsConnected,
  ShieldCheck,
  WarningCircle,
} from '@phosphor-icons/react'
import { getSkills, syncSkills, updateSkillsConfig } from '../api'
import { useStore } from '../store'
import PageHeader from '../components/PageHeader'
import type { SkillsStatusResponse, SkillsSyncTargetReport } from '../api/types'

// 技能面板：custom-skills 仓库是事实源，这里只做「启用清单 + 同步落盘」。
// 同步用符号链接把仓库里的 skills/<id> 铺进目标目录，不复制文件。

function parseTargets(input: string): string[] {
  return input
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function summarizeSync(report: SkillsSyncTargetReport[] | undefined): string {
  if (!report || report.length === 0) return ''
  return report
    .map((target) => {
      const parts = [
        target.linked.length > 0 ? `新装 ${target.linked.length}` : '',
        target.removed.length > 0 ? `清理 ${target.removed.length}` : '',
        target.skipped.length > 0 ? `跳过 ${target.skipped.length}` : '',
        target.errors.length > 0 ? `错误 ${target.errors.length}` : '',
      ].filter(Boolean)
      return parts.length > 0 ? `${target.target}：${parts.join('，')}` : `${target.target}：无变化`
    })
    .join(' / ')
}

export default function Skills() {
  const { apiKey } = useStore()
  const [status, setStatus] = useState<SkillsStatusResponse | null>(null)
  const [targetsInput, setTargetsInput] = useState('')
  const [enabled, setEnabled] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [feedback, setFeedback] = useState('')

  const refresh = useCallback(() => {
    if (!apiKey) return
    getSkills()
      .then((next) => {
        setStatus(next)
        setTargetsInput((next.targets ?? []).join(', '))
        setEnabled(new Set(next.enabled ?? []))
      })
      .catch(() => setStatus(null))
  }, [apiKey])

  useEffect(() => {
    refresh()
  }, [refresh])

  const skills = useMemo(() => {
    const rows = status?.skills ?? []
    const keyword = search.trim().toLowerCase()
    if (!keyword) return rows
    return rows.filter((row) =>
      `${row.id} ${row.displayName ?? ''} ${row.description ?? ''} ${(row.tags ?? []).join(' ')}`
        .toLowerCase()
        .includes(keyword),
    )
  }, [status, search])

  const dirty = useMemo(() => {
    if (!status) return false
    const serverTargets = (status.targets ?? []).join(', ')
    const serverEnabled = [...(status.enabled ?? [])].sort().join(',')
    return serverTargets !== targetsInput || serverEnabled !== [...enabled].sort().join(',')
  }, [status, targetsInput, enabled])

  const handleSave = async (withSync: boolean) => {
    setBusy(true)
    setError('')
    setFeedback('')
    try {
      let next = await updateSkillsConfig(parseTargets(targetsInput), [...enabled].sort())
      if (withSync) {
        next = await syncSkills()
        setFeedback(summarizeSync(next.sync?.targets) || '已同步')
      } else {
        setFeedback('清单已保存')
      }
      setStatus(next)
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  if (status && !status.configured) {
    return (
      <>
        <PageHeader title="技能" subtitle="custom-skills 技能市场" />
        <div className="settings-group">
          <div className="settings-item settings-stack">
            <div className="settings-heading">
              <div className="si-icon amber"><Lightning size={18} weight="duotone" aria-hidden="true" /></div>
              <div className="si-info">
                <div className="si-label">技能面板未启用</div>
                <div className="si-desc">{status.hint ?? '设置 SKILLS_REPO_PATH 后可用'}</div>
              </div>
            </div>
          </div>
        </div>
      </>
    )
  }

  return (
    <>
      <PageHeader title="技能" subtitle="custom-skills 技能市场 · 启用后同步到本地技能目录" />

      <div className="settings-group">
        <div className="sg-title">同步目标</div>
        <div className="settings-item settings-stack">
          <div className="settings-heading">
            <div className="si-icon accent"><PlugsConnected size={18} weight="duotone" aria-hidden="true" /></div>
            <div className="si-info">
              <div className="si-label">目标目录</div>
              <div className="si-desc">启用的技能会以符号链接铺进这些目录（逗号分隔，支持 ~）</div>
            </div>
            {status && (
              <span
                className={`status-badge ${status.in_sync ? 'ok' : 'degraded'}`}
                style={{ marginLeft: 'auto' }}
              >
                {status.in_sync ? (
                  <><CheckCircle size={13} weight="bold" aria-hidden="true" /> 已同步</>
                ) : (
                  <><WarningCircle size={13} weight="bold" aria-hidden="true" /> 待同步</>
                )}
              </span>
            )}
          </div>
          <input
            className="settings-input"
            type="text"
            placeholder="~/.agents/skills"
            value={targetsInput}
            onChange={(event) => setTargetsInput(event.target.value)}
          />
          {status?.manifest_path && <div className="settings-current">清单：{status.manifest_path}</div>}
          {(status?.stale_links?.length ?? 0) > 0 && (
            <div className="settings-current">
              待清理的旧链接：{status?.stale_links?.join('、')}
            </div>
          )}
          {error && <div className="settings-current">失败：{error}</div>}
          {feedback && !error && <div className="settings-current">{feedback}</div>}
          <div className="settings-control-row">
            <button
              className="button button-primary"
              disabled={busy || !dirty}
              onClick={() => handleSave(false)}
            >
              保存清单
            </button>
            <button
              className={`button ${busy ? '' : 'button-success'}`}
              disabled={busy}
              onClick={() => handleSave(true)}
            >
              {busy ? '处理中…' : dirty ? '保存并同步' : '重新同步'}
            </button>
            <span className="si-desc" style={{ alignSelf: 'center' }}>
              只建/删指向技能仓库的符号链接，不动目录里已有的其他文件
            </span>
          </div>
        </div>
      </div>

      <div className="settings-group">
        <div className="sg-title">
          技能目录{status?.skills ? `（${status.skills.length}）` : ''}
        </div>
        <div className="settings-control-row" style={{ marginBottom: 8 }}>
          <input
            className="settings-input"
            type="text"
            placeholder="搜索技能…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        {skills.map((skill) => {
          const installedCount = Object.values(skill.installed ?? {}).filter(Boolean).length
          return (
            <div key={skill.id} className="settings-item settings-stack">
              <div className="settings-heading">
                <div className="si-icon indigo" style={{ fontSize: 18 }}>
                  {skill.emoji || '🧩'}
                </div>
                <div className="si-info">
                  <div className="si-label">
                    {skill.displayName || skill.id}
                    <span className="si-desc" style={{ marginLeft: 6 }}>{skill.id}</span>
                  </div>
                  <div className="si-desc" title={skill.description}>
                    {(skill.description ?? '').split('\n')[0]}
                  </div>
                </div>
                <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 6 }}>
                  {(skill.tags ?? []).slice(0, 2).map((tag) => (
                    <span key={tag} className="capability-chip" style={{ cursor: 'default' }}>
                      {tag}
                    </span>
                  ))}
                  {skill.enabled && (
                    <span
                      className={`status-badge ${installedCount > 0 ? 'ok' : 'degraded'}`}
                      title={installedCount > 0 ? `已安装到 ${installedCount} 个目录` : '启用后需同步'}
                    >
                      {installedCount > 0 ? (
                        <><CheckCircle size={13} weight="bold" aria-hidden="true" /> ×{installedCount}</>
                      ) : (
                        <><WarningCircle size={13} weight="bold" aria-hidden="true" /> 待同步</>
                      )}
                    </span>
                  )}
                  <button
                    className={`capability-chip ${skill.enabled ? 'selected' : 'available'}`}
                    style={{ cursor: 'pointer' }}
                    disabled={busy}
                    onClick={() => {
                      const next = new Set(enabled)
                      if (next.has(skill.id)) {
                        next.delete(skill.id)
                      } else {
                        next.add(skill.id)
                      }
                      setEnabled(next)
                    }}
                  >
                    {skill.enabled ? '停用' : '启用'}
                  </button>
                </div>
              </div>
            </div>
          )
        })}
        {skills.length === 0 && (
          <div className="settings-item">
            <div className="si-info">
              <div className="si-desc">没有匹配的技能</div>
            </div>
          </div>
        )}
      </div>

      {status?.repo && (
        <div className="settings-group">
          <div className="settings-item">
            <div className="si-icon success"><ShieldCheck size={18} weight="duotone" aria-hidden="true" /></div>
            <div className="si-info">
              <div className="si-label">技能仓库</div>
              <div className="si-desc">{status.repo}</div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
