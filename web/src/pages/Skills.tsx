import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowClockwise, ArrowRight, Check, CheckCircle, FolderOpen, GlobeHemisphereWest, MagnifyingGlass, PuzzlePiece, WarningCircle } from '@phosphor-icons/react'
import { getSkills, syncSkills, updateSkillsConfig } from '../api'
import { useStore } from '../store'
import type { SkillsStatusResponse, SkillsSyncTargetReport } from '../api/types'
import './Skills.css'

function summarizeSync(reports: SkillsSyncTargetReport[] = []) {
  const count = (key: 'linked' | 'removed' | 'skipped' | 'errors') => reports.reduce((n, row) => n + (row[key]?.length ?? 0), 0)
  return { text: `已同步 · 新增 ${count('linked')}，移除 ${count('removed')}${count('skipped') ? `，保留 ${count('skipped')} 个同名已有技能` : ''}`, errors: reports.flatMap(row => row.errors ?? []) }
}
const parseTargets = (s: string) => s.split(/[,\n]/).map(v => v.trim()).filter(Boolean)

export default function Skills() {
  const { apiKey } = useStore()
  const [mode, setMode] = useState<'global' | 'project'>('global')
  const [project, setProject] = useState('')
  const [projectInput, setProjectInput] = useState(() => localStorage.getItem('skills.last-project') || '')
  const [status, setStatus] = useState<SkillsStatusResponse | null>(null)
  const [targets, setTargets] = useState('')
  const [enabled, setEnabled] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'enabled' | 'local'>('all')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [feedback, setFeedback] = useState('')
  const [reload, setReload] = useState(0)
  const scope = mode === 'project' ? project : ''
  const accept = useCallback((next: SkillsStatusResponse) => {
    setStatus(next); setTargets((next.targets ?? []).join(', ')); setEnabled(new Set(next.enabled ?? []))
  }, [])
  useEffect(() => {
    let cancelled = false
    setStatus(null); setError(''); setFeedback('')
    if (!apiKey || (mode === 'project' && !project)) { setLoading(false); return }
    setLoading(true)
    getSkills(scope).then(next => { if (!cancelled) accept(next) })
      .catch(() => { if (!cancelled) setError('无法读取技能配置，请检查项目路径、目录权限与网关连接。') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [apiKey, scope, mode, project, accept, reload])
  const dirty = !!status && (JSON.stringify(parseTargets(targets)) !== JSON.stringify(status.targets ?? []) || JSON.stringify([...enabled].sort()) !== JSON.stringify([...(status.enabled ?? [])].sort()))
  const rows = useMemo(() => (status?.skills ?? []).filter(row => {
    const match = `${row.id} ${row.displayName ?? ''} ${row.description ?? ''} ${(row.tags ?? []).join(' ')}`.toLowerCase().includes(search.trim().toLowerCase())
    return match && (filter !== 'enabled' || enabled.has(row.id))
  }), [status, search, enabled, filter])
  const locals = (status?.local_skills ?? []).filter(row => `${row.id} ${row.path}`.toLowerCase().includes(search.trim().toLowerCase()))
  const save = async (sync: boolean) => {
    setBusy(true); setError(''); setFeedback('')
    try {
      let next = status!
      if (dirty) { next = await updateSkillsConfig(parseTargets(targets), [...enabled].sort(), scope); accept(next) }
      if (sync) {
        next = await syncSkills(scope); accept(next)
        const result = summarizeSync(next.sync?.targets ?? [])
        if (result.errors.length) setError(`部分技能未同步：${result.errors.join('；')}`)
        else setFeedback(result.text)
      } else setFeedback('启用清单已保存，点击同步后生效。')
    } catch (err) { setError(`操作未完成，请检查目录权限后重试。${err instanceof Error ? ` (${err.message})` : ''}`) }
    finally { setBusy(false) }
  }
  const blocked = busy || dirty
  return <section className="skills-page" aria-labelledby="skills-title">
    <header className="skills-heading">
      <div><h1 id="skills-title">技能库</h1><p>挑选常用能力，也为每个项目保留自己的工具箱。</p></div>
      <button className="button skills-quiet" disabled={blocked || loading} onClick={() => setReload(v => v + 1)}><ArrowClockwise size={16} />刷新</button>
    </header>
    <div className="skills-layout">
      <aside className="skills-scope" aria-label="技能作用域">
        <h2>应用范围</h2>
        <button className={`scope-option ${mode === 'global' ? 'active' : ''}`} aria-pressed={mode === 'global'} disabled={blocked} onClick={() => setMode('global')}><GlobeHemisphereWest size={20} /><span><strong>全局技能</strong><small>在多个项目中使用</small></span></button>
        <button className={`scope-option ${mode === 'project' ? 'active' : ''}`} aria-pressed={mode === 'project'} disabled={blocked} onClick={() => setMode('project')}><FolderOpen size={20} /><span><strong>项目技能</strong><small>为当前项目单独配置</small></span></button>
        {mode === 'project' && <form className="skills-project-form" onSubmit={e => { e.preventDefault(); setProject(projectInput.trim()); localStorage.setItem('skills.last-project', projectInput.trim()); setReload(v => v + 1) }}>
          <label htmlFor="skills-project-path">项目路径</label><input id="skills-project-path" value={projectInput} placeholder="/完整路径/项目" disabled={blocked} onChange={e => setProjectInput(e.target.value)} />
          <button className="button skills-quiet" disabled={blocked || !projectInput.trim()}>打开项目<ArrowRight size={14} /></button>
          <p>每个项目独立保存启用清单，仅同步至项目的 .agents/skills。</p>
        </form>}
        <div className="skills-scope-note"><PuzzlePiece size={18} /><p>技能库提供可复用的能力。项目已有的自定义技能可在「本地已有」中查看。</p></div>
      </aside>
      <div className="skills-main">
        {error && <div className="skills-notice error" role="alert"><WarningCircle size={18}/><span>{error}</span></div>}
        {feedback && !error && <div className="skills-notice" role="status"><CheckCircle size={18}/><span>{feedback}</span></div>}
        {loading ? <div className="skills-loading" role="status"><span>正在读取技能…</span><div/><div/><div/></div> : !status ? <div className="skills-empty"><FolderOpen size={32}/><h2>{mode === 'project' && !project ? '先选择一个项目' : '暂时无法加载'}</h2><p>{mode === 'project' && !project ? '输入本机项目路径，查看已有技能并选择这个项目需要的能力。' : '检查连接或项目路径，然后重试。'}</p></div> : !status.configured ? <div className="skills-empty"><PuzzlePiece size={32}/><h2>连接你的技能库</h2><p>{status.hint || '配置 SKILLS_REPO_PATH 后即可浏览和管理技能。'}</p></div> : <>
          <div className="skills-context">
            <div><h2>{mode === 'global' ? '全局工作环境' : (status.project || project).split('/').filter(Boolean).pop()}</h2><p>{mode === 'global' ? '通用技能随时可用，项目能力按需添加。' : '这里的选择只影响这个项目，全局技能的加载方式由各客户端决定。'}</p></div>
            <span className={`skills-state ${status.in_sync && !dirty ? 'synced' : ''}`}>{dirty ? '有未保存更改' : status.in_sync ? <><Check size={14}/>已同步</> : '有待同步更改'}</span>
          </div>
          <div className="skills-toolbar">
            <div className="skills-filters" aria-label="筛选技能">
              <button aria-pressed={filter === 'all'} onClick={() => setFilter('all')}>技能目录 <span>{status.skills?.length ?? 0}</span></button>
              <button aria-pressed={filter === 'enabled'} onClick={() => setFilter('enabled')}>已选择 <span>{enabled.size}</span></button>
              <button aria-pressed={filter === 'local'} onClick={() => setFilter('local')}>本地已有 <span>{status.local_skills?.length ?? 0}</span></button>
            </div>
            <label className="skills-search"><MagnifyingGlass size={18}/><input aria-label="搜索技能" placeholder="搜索名称、描述或标签" value={search} onChange={e => setSearch(e.target.value)}/></label>
          </div>
          {filter === 'local' ? <div className="skills-list">
            <p className="skills-local-note">这些技能来自当前目录或其他安装来源，仅供查看；同步不会覆盖它们。</p>
            {locals.map(row => <article className="skill-row local" key={row.path}><div className="skill-glyph"><FolderOpen size={20}/></div><div className="skill-copy"><h3>{row.id}</h3><p className="skill-path">{row.path}</p></div><span className="skill-label">{row.source} · 已有技能</span></article>)}
            {!locals.length && <div className="skills-empty compact"><p>没有发现匹配的本地自定义技能。</p></div>}
          </div> : <div className="skills-list">
            {rows.map(skill => {
              const selected = enabled.has(skill.id)
              const installed = (status.targets?.length ?? 0) > 0 && (status.targets ?? []).every(target => skill.installed?.[target])
              return <article className={`skill-row ${selected ? 'selected' : ''}`} key={skill.id}>
                <div className="skill-glyph"><PuzzlePiece size={22}/></div>
                <div className="skill-copy"><div className="skill-name"><h3>{skill.displayName || skill.id}</h3>{selected && <span className="skill-label">{installed ? '已安装' : '待同步'}</span>}</div>
                  <details className="skill-description"><summary>{skill.description?.split('\n')[0] || skill.id}</summary><p>{skill.description || '暂无详细描述'}</p><code>{skill.id}</code></details>
                  <div className="skill-tags">{(skill.tags ?? []).slice(0, 3).map(tag => <span key={tag}>{tag}</span>)}</div>
                </div>
                <button className={`skill-toggle ${selected ? 'on' : ''}`} aria-pressed={selected} aria-label={`${selected ? '取消选择' : '选择'} ${skill.displayName || skill.id}`} disabled={busy} onClick={() => setEnabled(prev => { const next = new Set(prev); if (next.has(skill.id)) next.delete(skill.id); else next.add(skill.id); return next })}>{selected ? <Check size={15}/> : <span>+</span>}{selected ? '已选择' : '选择'}</button>
              </article>
            })}
            {!rows.length && <div className="skills-empty compact"><MagnifyingGlass size={24}/><p>{search ? '没有匹配的技能，试试其他关键词。' : '还没有选择技能，从技能目录中添加。'}</p>{search && <button className="button skills-quiet" onClick={() => setSearch('')}>清除搜索</button>}</div>}
          </div>}
          <details className="skills-advanced"><summary>安装位置与清单</summary><label htmlFor="skills-targets">{mode === 'project' ? '项目安装目录' : '全局安装目录（多个目录用逗号分隔）'}</label><input id="skills-targets" value={targets} disabled={busy || mode === 'project'} onChange={e => setTargets(e.target.value)}/><p>清单：{status.manifest_path}</p><p>技能来源：{status.repo}</p></details>
          <footer className="skills-savebar"><div><strong>{enabled.size} 个技能已选择</strong><small>{dirty ? '保存后同步生效；切换范围前请保存或撤销。' : status.in_sync ? '当前清单与安装状态一致' : '同步会更新技能库链接，保留已有自定义文件'}</small></div><div className="skills-save-actions">{dirty && <button className="button skills-quiet" disabled={busy} onClick={() => { accept(status); setError(''); setFeedback('') }}>撤销</button>}<button className="button skills-quiet" disabled={busy || !dirty || !parseTargets(targets).length} onClick={() => save(false)}>仅保存</button><button className="button skills-primary" disabled={busy || !parseTargets(targets).length} onClick={() => save(true)}>{busy ? '正在处理…' : dirty ? '保存并同步' : '同步技能'}</button></div></footer>
        </>}
      </div>
    </div>
  </section>
}
