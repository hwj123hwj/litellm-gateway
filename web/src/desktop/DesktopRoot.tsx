import { useEffect, useState } from 'react'
import { ArrowLeft, ArrowRight, CheckCircle, Desktop, Globe, PencilSimple, Plus, Trash } from '@phosphor-icons/react'
import App from '../App'
import { useStore } from '../store'
import { manageConnections, nativeCall } from './bridge'
import type { ConnectionInput, ConnectionState } from './bridge'
import './desktop.css'

const empty: ConnectionInput = { id: '', name: '本地网关', url: 'http://localhost:4001', token: '' }

export default function DesktopRoot() {
  const [config, setConfig] = useState<ConnectionState | null>(null)
  const [route, setRoute] = useState(location.hash)
  const [form, setForm] = useState<ConnectionInput>(empty)
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [tested, setTested] = useState(false)

  useEffect(() => {
    nativeCall<ConnectionState>('Config').then(setConfig).catch(e => setError(e.message))
    const onRoute = () => setRoute(location.hash)
    addEventListener('hashchange', onRoute)
    return () => removeEventListener('hashchange', onRoute)
  }, [])

  const active = config?.profiles.find(p => p.id === config.activeId)
  const showConnections = !active || route.startsWith('#/connections')
  // Auth and routing are owned by Go in the desktop, not browser storage.
  useEffect(() => {
    useStore.setState({ apiKey: active ? 'desktop-managed' : '', backendUrl: active?.url || '' })
  }, [active?.id, active?.url])

  const update = (key: keyof ConnectionInput, value: string) => {
    setForm(p => ({ ...p, [key]: value })); setTested(false); setError('')
  }
  const run = async (operation: () => Promise<void>) => {
    setBusy(true); setError('')
    try { await operation() } catch (e) { setError(e instanceof Error ? e.message : '连接操作失败') }
    finally { setBusy(false) }
  }
  const connect = () => run(async () => {
    const next = await nativeCall<ConnectionState>('Save', form)
    setConfig(next); setForm(empty); setEditing(false); setTested(false)
    // Reload clears in-flight requests and cached data from the previous host.
    location.hash = '/'; location.reload()
  })

  return <div className="desktop-root">
    <header className="desktop-titlebar">
      <span className="desktop-title">Gateway</span>
      <div className="desktop-titlebar-actions">
        {active && <button type="button" className="desktop-connection-button" onClick={manageConnections} title="管理网关连接">
          <Desktop size={16} aria-hidden="true" /><span>{active.name}</span>
        </button>}
      </div>
    </header>
    {showConnections ? <main className="desktop-connections">
      {active && <button className="desktop-back" type="button" onClick={() => { location.hash = '/' }}><ArrowLeft size={16} />返回控制台</button>}
      <div className="desktop-connect-heading"><div className="desktop-monogram" aria-hidden="true">G</div>
        <h1>连接你的网关</h1><p>在一个窗口管理本地和远程网关的模型、供应商与运行状态。</p>
      </div>
      {error && <div className="error-banner" role="alert">{error}{!config && <button className="button button-secondary" type="button" disabled={busy} onClick={() => run(async () => setConfig(await nativeCall<ConnectionState>('Config')))}>重新读取</button>}</div>}
      {!config && !error && <p role="status">正在读取连接配置…</p>}
      {!!config?.profiles.length && <section className="desktop-profile-list" aria-label="已保存的网关">
        {config.profiles.map(p => <div className="desktop-profile" key={p.id}>
          <Globe size={22} aria-hidden="true" />
          <button className="desktop-profile-select" type="button" disabled={busy} onClick={() => run(async () => {
            await nativeCall('Select', p.id); location.hash = '/'; location.reload()
          })}><strong>{p.name}</strong><span>{p.url}</span></button>
          {p.id === active?.id && <span className="status-badge ok">当前连接</span>}
          <button className="icon-btn" type="button" disabled={busy} aria-label={`编辑 ${p.name}`} onClick={() => {
            setForm({ id: p.id, name: p.name, url: p.url, token: '' }); setEditing(true); setError(''); setTested(false)
          }}><PencilSimple size={18} /></button>
          <button className="icon-btn" type="button" disabled={busy} aria-label={`移除 ${p.name}`} onClick={() => {
            if (window.confirm(`移除“${p.name}”的连接配置？网关服务将继续运行。`)) void run(async () => {
              setConfig(await nativeCall<ConnectionState>('Delete', p.id)); setEditing(false); setForm(empty)
              // Unmount stale host pages before the next active connection renders.
              location.hash = '/connections'; location.reload()
            })
          }}><Trash size={18} /></button>
        </div>)}
      </section>}
      {!editing && !!config?.profiles.length ? <button className="button button-secondary desktop-add" type="button" onClick={() => {setEditing(true); setForm({...empty, name: '', url: ''}); setError(''); setTested(false)}}><Plus size={16} />添加网关连接</button>
      : config && <form className="desktop-connection-form" onSubmit={e => {e.preventDefault(); void connect()}}>
        <h2>{form.id ? '编辑连接' : '添加网关连接'}</h2>
        <fieldset disabled={busy}>
          <label htmlFor="connection-name">连接名称</label><input id="connection-name" className="settings-input" value={form.name} onChange={e => update('name', e.target.value)} placeholder="例如：本地网关、Mini PC" required maxLength={80} />
          <label htmlFor="connection-url">网关地址</label><input id="connection-url" className="settings-input" type="url" value={form.url} onChange={e => update('url', e.target.value)} placeholder="http://localhost:4001" required autoCapitalize="none" spellCheck={false} />
          <label htmlFor="connection-token">管理 Token</label><input id="connection-token" className="settings-input" type="password" value={form.token} onChange={e => update('token', e.target.value)} placeholder={form.id ? '留空保留已保存的 Token' : 'ADMIN_TOKEN 或 LITELLM_MASTER_KEY'} required={!form.id} autoComplete="off" />
          <p className="desktop-token-note">Token 仅保存在这台电脑的受保护配置文件中，由桌面端附加到网关请求。</p>
          {form.url.startsWith('http://') && !/^http:\/\/(localhost|127\.0\.0\.1|\[::1\])(:|\/|$)/.test(form.url) && <p className="desktop-http-note">此远程连接使用 HTTP，管理 Token 会以明文传输。建议使用 HTTPS。</p>}
          {tested && <p className="desktop-test-success" role="status"><CheckCircle size={16} />连接正常，管理 Token 已验证</p>}
          <div className="desktop-form-actions">
            <button className="button button-secondary" type="button" onClick={() => run(async () => {await nativeCall('Test', form); setTested(true)})}>测试连接</button>
            {!!config.profiles.length && <button className="button button-secondary" type="button" onClick={() => {setEditing(false); setForm(empty); setError('')}}>取消</button>}
            <button className="button button-primary" type="submit">{busy ? '正在连接…' : '保存并连接'}<ArrowRight size={16} /></button>
          </div>
        </fieldset>
      </form>}
    </main> : <App key={active.id} desktop />}
  </div>
}
