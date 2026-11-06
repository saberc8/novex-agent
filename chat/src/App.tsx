import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowUp, BookOpen, Check, ChevronDown, ChevronRight, CircleHelp, FileSpreadsheet, FileText, FolderOpen, LoaderCircle, LogOut, Menu, MessageSquare, PanelLeftClose, PanelRight, Plus, Search, ShieldCheck, Sparkles, Square, Upload, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import SpreadsheetViewer from './SpreadsheetViewer'
import { api, ApiError, setToken, streamRun, type Conversation, type Document, type Message, type Runtime, type User } from './api'

const compact = new URLSearchParams(location.search).get('embed') === '1'
const allowedParents = (import.meta.env.VITE_EMBED_ALLOWED_ORIGINS || 'http://localhost:8311,http://127.0.0.1:8311').split(',').map((s: string) => s.trim())
const expectedParent = new URLSearchParams(location.search).get('parentOrigin') || ''
const safeParent = allowedParents.includes(expectedParent) ? expectedParent : ''
const scopeName: Record<string, string> = { self: '仅拥有者', department: '所属部门', tenant: '所属企业' }
const errorText = (error: unknown) => error instanceof Error ? error.message : '操作未完成，请重试'
function Markdown({ children }: { children: string }) { return <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ a: ({ children, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer">{children}</a>, img: ({ alt }) => <span className="subtle">[图片：{alt || '附件'}]</span> }}>{children}</ReactMarkdown> }

export default function App() {
  const [user, setUser] = useState<User | null>(null)
  const [booting, setBooting] = useState(true)
  const [error, setError] = useState('')
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [current, setCurrent] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [runtimes, setRuntimes] = useState<Runtime[]>([])
  const [runtime, setRuntime] = useState('pi')
  const [documents, setDocuments] = useState<Document[]>([])
  const [document, setDocument] = useState<Document | null>(null)
  const [input, setInput] = useState('')
  const [query, setQuery] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [sideOpen, setSideOpen] = useState(!compact)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [runId, setRunId] = useState('')
  const [activities, setActivities] = useState<string[]>([])
  const controller = useRef<AbortController | null>(null)
  const authGeneration = useRef(0)
  const historyGeneration = useRef(0)
  const bottom = useRef<HTMLDivElement>(null)
  const composer = useRef<HTMLTextAreaElement>(null)

  async function refresh() {
    const generation = authGeneration.current
    const results = await Promise.allSettled([api<Conversation[]>('/conversations'), api<Runtime[]>('/runtimes'), api<Document[]>('/knowledge'), api<User>('/me')])
    if (generation !== authGeneration.current) return
    if (results[0].status === 'fulfilled') { const items = results[0].value || []; setConversations(items); setCurrent(value => value ? items.find(item => item.id === value.id) || value : null) }
    if (results[1].status === 'fulfilled') setRuntimes(results[1].value || [])
    if (results[2].status === 'fulfilled') setDocuments(results[2].value || [])
    if (results[3].status === 'fulfilled') setUser(results[3].value)
    const failed = results.find(r => r.status === 'rejected')
    if (failed?.status === 'rejected') setError(errorText(failed.reason))
  }
  function authenticated(result: { token: string; user: User }) {
    authGeneration.current += 1
    setToken(result.token)
    if (!compact) sessionStorage.setItem('novex.session', result.token)
    setUser(result.user)
    setError('')
  }
  useEffect(() => {
    let alive = true
    const saved = !compact && sessionStorage.getItem('novex.session')
    if (saved) {
      setToken(saved)
      api<User>('/me').then(data => { if (alive) setUser(data) }).catch(() => { sessionStorage.removeItem('novex.session'); setToken('') }).finally(() => { if (alive) setBooting(false) })
    } else setBooting(false)
    return () => { alive = false; controller.current?.abort() }
  }, [])
  useEffect(() => { if (user) void refresh() }, [user?.id])
  useEffect(() => { bottom.current?.scrollIntoView({ behavior: 'smooth', block: 'end' }) }, [messages, activities, busy])
  useEffect(() => {
    if (!compact || !safeParent || window.parent === window) return
    let exchanging = false
    const receive = async (event: MessageEvent) => {
      if (event.source !== window.parent || event.origin !== safeParent || event.data?.type !== 'novex:launch' || typeof event.data.ticket !== 'string' || exchanging) return
      exchanging = true
      setBooting(true)
      try {
        authenticated(await api<{ token: string; user: User }>('/embed-exchange', 'POST', { ticket: event.data.ticket, origin: safeParent }))
        window.parent.postMessage({ type: 'novex:authenticated' }, safeParent)
      } catch (e) { setError(errorText(e)); exchanging = false }
      finally { setBooting(false) }
    }
    window.addEventListener('message', receive)
    window.parent.postMessage({ type: 'novex:ready' }, safeParent)
    return () => window.removeEventListener('message', receive)
  }, [])

  async function logout() {
    authGeneration.current += 1
    controller.current?.abort()
    try { await api('/logout', 'POST', {}) } catch { /* Clear local credentials even if the network is unavailable. */ }
    finally { setToken(''); sessionStorage.removeItem('novex.session'); setUser(null); setMessages([]); setCurrent(null); setConversations([]); setDocuments([]); setDocument(null); setBusy(false) }
  }
  function newConversation() { if (busy) return; historyGeneration.current += 1; setLoadingHistory(false); setCurrent(null); setMessages([]); setError(''); setActivities([]); setRunId(''); composer.current?.focus(); if (innerWidth < 900) setSideOpen(false) }
  async function openConversation(item: Conversation) {
    if (busy) return
    const history = ++historyGeneration.current
    const generation = authGeneration.current
    setCurrent(item); setLoadingHistory(true); setMessages([]); setError(''); setRuntime(item.runtime); setActivities([]); setRunId('')
    if (innerWidth < 900) setSideOpen(false)
    try { const result = await api<Message[]>(`/conversations/${item.id}/messages`); if (history === historyGeneration.current && generation === authGeneration.current) setMessages(result || []) }
    catch (e) { if (history === historyGeneration.current && generation === authGeneration.current) { setError(errorText(e)); if (e instanceof ApiError && e.status === 409) setCurrent({ ...item, locked: true }) } }
    finally { if (history === historyGeneration.current && generation === authGeneration.current) setLoadingHistory(false) }
  }
  async function send(event?: FormEvent) {
    event?.preventDefault()
    const content = input.trim()
    const generation = authGeneration.current
    if (!content || busy || current?.locked) return
    setBusy(true); setError(''); setActivities(['正在建立安全会话…'])
    let conversation = current
    let started = false
    try {
      if (!conversation) {
        conversation = await api<Conversation>('/conversations', 'POST', { title: content.slice(0, 35), runtime })
        setCurrent(conversation)
        setConversations(items => [conversation!, ...items])
      }
      const result = await api<{ runId: string }>(`/conversations/${conversation.id}/messages`, 'POST', { content })
      started = true
      setInput(''); setRunId(result.runId)
      setMessages(items => [...items, { id: `pending-${result.runId}`, role: 'user', content, createdAt: new Date().toISOString(), runId: result.runId }])
      controller.current = new AbortController()
      let terminal = false
      await streamRun(result.runId, controller.current.signal, (name, data) => {
        if (generation !== authGeneration.current) return
        if (name === 'status') setActivities(items => [...items, String(data.message || '正在处理…')].slice(-20))
        if (name === 'completed') { terminal = true; setMessages(items => [...items, { id: `answer-${result.runId}`, role: 'assistant', content: String(data.text || ''), createdAt: new Date().toISOString(), runId: result.runId }]); setActivities(items => [...items, '回答已完成']) }
        if (name === 'error') { terminal = true; setError(String(data.message || '本次运行未完成')) }
      })
      if (!terminal && !controller.current.signal.aborted) {
        const run = await api<{ status: string; answer?: string; error?: string }>(`/runs/${result.runId}`)
        if (run.answer) setMessages(items => [...items, { id: `answer-${result.runId}`, role: 'assistant', content: run.answer!, createdAt: new Date().toISOString(), runId: result.runId }])
        else throw new Error(run.error || '状态连接已中断，请打开历史对话查看结果。')
      }
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) setError(errorText(e))
      if (e instanceof ApiError && e.status === 409 && conversation) setCurrent({ ...conversation, locked: true })
      if (!started) setActivities([])
    } finally { if (generation === authGeneration.current) { setBusy(false); controller.current = null; void refresh() } }
  }
  async function cancel() {
    if (!runId) return
    try { await api(`/runs/${runId}/cancel`, 'POST', {}); controller.current?.abort(); setActivities(items => [...items, '已停止本次回答']); setBusy(false) } catch (e) { setError(errorText(e)) }
  }
  async function openDocument(item: Document) {
    const generation = authGeneration.current
    try { const result = await api<Document>(`/knowledge/${item.id}`); if (generation === authGeneration.current) setDocument(result) } catch (e) { if (generation === authGeneration.current) setError(errorText(e)) }
  }

  if (booting) return <div className="boot-screen"><LoaderCircle className="spin" size={24}/><span>正在连接 Novex</span></div>
  if (!user) return <Login onLogin={authenticated} initialError={error} embedded={compact}/>

  return <div className={`app ${compact ? 'compact' : ''} ${sideOpen ? 'sidebar-open' : ''}`}>
    {sideOpen && <button className="sidebar-scrim" aria-label="关闭侧栏" onClick={() => setSideOpen(false)}/>}
    <aside className={`sidebar ${sideOpen ? 'visible' : ''}`}>
      <div className="brand"><span className="brand-mark"><Sparkles size={19}/></span><strong>Novex</strong><button className="icon-button muted" title="收起侧栏" onClick={() => setSideOpen(false)}><PanelLeftClose size={19}/></button></div>
      <button className="nav-action" onClick={newConversation} disabled={busy}><Plus size={19}/><span>新对话</span><kbd>＋</kbd></button>
      <button className="nav-action" onClick={() => setSearchOpen(!searchOpen)}><Search size={18}/><span>搜索对话</span></button>
      <button className="nav-action" onClick={() => { setDetailsOpen(true); if (innerWidth < 900) setSideOpen(false) }}><BookOpen size={18}/><span>我的知识库</span><span className="count">{documents.length}</span></button>
      <div className="sidebar-section"><span>最近对话</span><span>{conversations.length}</span></div>
      {searchOpen && <div className="search-box"><Search size={15}/><input autoFocus aria-label="搜索对话标题" placeholder="搜索标题" value={query} onChange={e => setQuery(e.target.value)}/></div>}
      <div className="conversation-list">
        {conversations.filter(item => item.title.toLowerCase().includes(query.toLowerCase())).map(item => <button disabled={busy} key={item.id} className={`conversation-item ${current?.id === item.id ? 'active' : ''}`} onClick={() => void openConversation(item)}><MessageSquare size={15}/><span>{item.title || '未命名对话'}</span>{item.locked && <ShieldCheck size={13}/>}</button>)}
        {!conversations.length && <p className="empty-history">从一个问题开始，<br/>对话会保存在这里。</p>}
      </div>
      <div className="sidebar-bottom"><div className="avatar">{(user.nickname || user.username).slice(0, 1).toUpperCase()}</div><div className="user-info"><strong>{user.nickname || user.username}</strong><span>{user.roles?.map(role => role.name).join(' · ') || '已登录'}</span></div><button className="icon-button muted" title="退出登录" onClick={logout}><LogOut size={17}/></button></div>
    </aside>

    <main className="chat-main">
      <header className="chat-header"><div className="header-title">{!sideOpen && <button className="icon-button" title="展开侧栏" onClick={() => setSideOpen(true)}><Menu size={20}/></button>}<span>{current?.title || '新对话'}</span>{current?.locked && <span className="locked-tag">权限已更新</span>}</div><div className="header-actions"><span className="private-label"><ShieldCheck size={14}/> 私密工作空间</span><button className={`icon-button ${detailsOpen ? 'selected' : ''}`} title="查看详情与资料" onClick={() => setDetailsOpen(!detailsOpen)}><PanelRight size={19}/></button></div></header>
      <div className="message-scroll">
        <div className={`message-column ${!messages.length ? 'empty' : ''}`}>
          {loadingHistory ? <div className="history-loading"><LoaderCircle size={22} className="spin"/>正在加载对话</div> : !messages.length && <div className="welcome"><div className="welcome-eyebrow"><span className="small-mark"><Sparkles size={18}/></span> YOUR WORK, CONNECTED</div><h1>让知识，为你所用。</h1><p>询问工作中的问题，从资料和业务数据中找到答案。</p><div className="suggestions">{[{ icon: <BookOpen size={18}/>, title: '查阅知识', text: '有哪些我可以查阅的知识资料？' }, { icon: <FolderOpen size={18}/>, title: '了解业务', text: '我可以访问哪些业务数据？' }, { icon: <CircleHelp size={18}/>, title: '开始探索', text: '你能如何协助我的工作？' }].map(s => <button key={s.title} onClick={() => { setInput(s.text); composer.current?.focus() }}>{s.icon}<strong>{s.title}</strong><ChevronRight size={15}/></button>)}</div></div>}
          {messages.map(message => <article key={message.id} className={`message ${message.role}`}><div className="message-label">{message.role === 'assistant' ? <><span className="small-mark"><Sparkles size={14}/></span>Novex</> : '你'}</div><div className="message-content"><Markdown>{message.content}</Markdown></div>{message.role === 'assistant' && message.runId && <div className="answer-meta"><Check size={12}/> 已完成<span>·</span><button onClick={() => setDetailsOpen(true)}>查看详情</button></div>}</article>)}
          {busy && <div className="working-state"><LoaderCircle size={16} className="spin"/><span>{activities[activities.length - 1] || '正在思考…'}</span></div>}
          <div ref={bottom}/>
        </div>
      </div>
      <div className="composer-area"><div className="composer-column">
        {error && <div className="error-notice" role="alert"><span>{error}</span><button title="关闭提示" className="icon-button" onClick={() => setError('')}><X size={15}/></button></div>}
        {current?.locked ? <div className="locked-conversation"><ShieldCheck size={18}/><div><strong>这个对话的权限范围已经变化</strong><p>新建对话后，将按你当前的权限继续回答。</p></div><button className="primary-button" onClick={newConversation}>新建对话</button></div> : <form className="composer" onSubmit={send}>
          <textarea ref={composer} rows={2} placeholder="向 Novex 提问…" aria-label="消息" value={input} onChange={e => setInput(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send() } }} disabled={loadingHistory}/>
          <div className="composer-toolbar"><div className="composer-left"><button type="button" className="icon-button" title="上传知识文件" onClick={() => setUploadOpen(true)}><Plus size={21}/></button><span className="access-label"><ShieldCheck size={13}/> 仅检索已授权内容</span></div><div className="composer-right"><label className="runtime-select"><select aria-label="选择智能体" value={current?.runtime || runtime} disabled={busy || !!current} onChange={e => setRuntime(e.target.value)}>{(runtimes.length ? runtimes : [{ id: 'pi', name: 'Pi', available: false }, { id: 'dsh', name: 'DeepSeek Harness', available: false }]).map(item => <option key={item.id} value={item.id}>{item.name}{!item.available ? ' · 未就绪' : ''}</option>)}</select><ChevronDown size={13}/></label>{busy ? <button type="button" className="send-button" title="停止生成" onClick={() => void cancel()}><Square size={15} fill="currentColor"/></button> : <button type="submit" className="send-button" title="发送消息" disabled={!input.trim() || loadingHistory}><ArrowUp size={20}/></button>}</div></div>
        </form>}
        <div className="composer-footnote">回答依据你有权访问的资料。重要信息请核对原始来源。</div>
      </div></div>
    </main>

    {detailsOpen && <aside className="details-panel"><div className="details-heading"><h2>对话详情</h2><button className="icon-button" title="关闭详情" onClick={() => setDetailsOpen(false)}><X size={17}/></button></div><div className="detail-section"><h3>工作空间</h3><div className="security-card"><ShieldCheck size={18}/><div><strong>{user.globalDataAccess ? '全局数据管理员' : '权限隔离已启用'}</strong><p>{user.nickname || user.username} · {user.globalDataAccess ? '全部企业 · 全部知识资料与已注册业务资源' : user.tenantId || '当前组织'}</p><span>权限版本 {user.policyVersion}</span></div></div></div><div className="detail-section"><div className="section-title"><h3>可用资料</h3><button className="icon-button" title="上传资料" onClick={() => setUploadOpen(true)}><Plus size={17}/></button></div>{documents.length ? documents.map(item => <button key={item.id} className="document-row" onClick={() => void openDocument(item)}><>{item.kind === 'spreadsheet' ? <FileSpreadsheet size={17}/> : <FileText size={17}/>}</><span><strong>{item.title || item.fileName}</strong><small>{user.globalDataAccess && item.tenantId ? `${item.tenantId} · ` : ''}{scopeName[item.scope] || item.scope} · {item.kind === 'spreadsheet' ? `${item.fileName.toLowerCase().endsWith('.csv') ? 'CSV' : 'Excel'} · ${item.sheetCount ?? 0} 张表 · ${(item.rowCount ?? 0).toLocaleString()} 行` : `${(item.characters ?? 0).toLocaleString()} 字`}</small></span><ChevronRight size={13}/></button>) : <div className="detail-empty"><BookOpen size={23}/><p>还没有可查阅的资料</p><button onClick={() => setUploadOpen(true)}>上传第一份文件</button></div>}</div><div className="detail-section"><h3>运行动态</h3>{activities.length ? <ol className="activity-list">{activities.map((text, index) => <li key={`${index}-${text}`}><span className={`activity-dot ${busy && index === activities.length - 1 ? 'running' : ''}`}/>{text}</li>)}</ol> : <p className="subtle">发起问题后，处理进展会显示在这里。</p>}{runId && <p className="run-id">运行 {runId}</p>}</div></aside>}

    {uploadOpen && <UploadDialog isAdmin={user.isAdmin} onClose={() => setUploadOpen(false)} onSaved={() => { setUploadOpen(false); void refresh(); setDetailsOpen(true) }}/ >}
    {document && <div className="modal-backdrop" onClick={() => setDocument(null)}><section className={`document-modal ${document.kind === 'spreadsheet' ? 'spreadsheet-modal' : ''}`} role="dialog" aria-modal="true" aria-label={document.title} onClick={e => e.stopPropagation()}><header><div><span className="eyebrow">知识资料</span><h2>{document.title || document.fileName}</h2></div><button className="icon-button" title="关闭资料" onClick={() => setDocument(null)}><X size={20}/></button></header><div className="document-body">{document.kind === 'spreadsheet' ? <SpreadsheetViewer key={document.id} document={document}/> : <Markdown>{document.markdown || '这份资料尚无可展示的内容。'}</Markdown>}</div><footer><FileText size={14}/>{document.fileName}<span>{document.tenantId ? `${document.tenantId} · ` : ''}{scopeName[document.scope]}</span></footer></section></div>}
  </div>
}

function Login({ onLogin, initialError, embedded }: { onLogin: (result: { token: string; user: User }) => void; initialError: string; embedded: boolean }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(initialError)
  useEffect(() => setError(initialError), [initialError])
  async function submit(event: FormEvent) { event.preventDefault(); setBusy(true); setError(''); try { onLogin(await api<{ token: string; user: User }>('/login', 'POST', { username, password })) } catch (e) { setError(errorText(e)) } finally { setBusy(false) } }
  return <div className="login-page"><div className="login-brand"><span className="brand-mark"><Sparkles size={22}/></span>Novex</div><main className="login-card"><span className="eyebrow">你的工作知识，触手可及</span><h1>欢迎回来。</h1><p>登录你的工作账号，开始一次有依据的对话。</p><form onSubmit={submit}><label>账号<input autoComplete="username" placeholder="输入账号" value={username} onChange={e => setUsername(e.target.value)} required/></label><label>密码<input type="password" autoComplete="current-password" placeholder="输入密码" value={password} onChange={e => setPassword(e.target.value)} required/></label>{error && <div className="login-error" role="alert">{error}</div>}<button className="primary-button" disabled={busy}>{busy ? <LoaderCircle size={17} className="spin"/> : <>登录工作空间<ArrowUp size={17}/></>}</button></form><div className="login-security"><ShieldCheck size={15}/>{embedded ? '也可由嵌入页面安全登录' : '每次查询都遵循你的账号权限'}</div></main><footer>Novex · 连接知识与业务</footer></div>
}

function UploadDialog({ isAdmin, onClose, onSaved }: { isAdmin: boolean; onClose: () => void; onSaved: () => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('')
  const [scope, setScope] = useState('self')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function submit(event: FormEvent) {
    event.preventDefault(); if (!file) return
    setBusy(true); setError('')
    const data = new FormData(); data.append('file', file); data.append('title', title); data.append('scope', scope)
    try { await api('/knowledge', 'POST', data); onSaved() } catch (e) { setError(errorText(e)) } finally { setBusy(false) }
  }
  return <div className="modal-backdrop"><section className="upload-modal" role="dialog" aria-modal="true" aria-label="上传知识文件"><header><h2>添加知识资料</h2><button className="icon-button" title="关闭上传" onClick={onClose} disabled={busy}><X size={19}/></button></header><p>文档转换为可检索资料；Excel / CSV 保留原文件，直接按工作表与数据行检索。</p><form onSubmit={submit}><label className="file-drop"><Upload size={25}/><strong>{file ? file.name : '选择要上传的文件'}</strong><span>MD、TXT、DOCX、文字型 PDF、XLSX、CSV</span><input aria-label="选择知识文件" type="file" accept=".md,.markdown,.txt,.docx,.pdf,.xlsx,.csv" onChange={e => setFile(e.target.files?.[0] || null)} required/></label><p className="upload-sheet-limit">表格文件最多 32 MiB、20 万数据行；不执行公式。不支持 XLS / XLSM。</p><p className="upload-sheet-limit">Excel 权限作用于整个文件和全部工作表；需要按员工、部门或字段隔离时，请接入业务数据库配置数据授权。</p><label className="form-label">资料名称<input placeholder="留空则使用文件名" value={title} onChange={e => setTitle(e.target.value)}/></label><label className="form-label">谁可以查阅<select value={scope} onChange={e => setScope(e.target.value)}><option value="self">仅自己</option>{isAdmin && <><option value="department">本部门</option><option value="tenant">本企业</option></>}</select></label>{error && <div className="login-error" role="alert">{error}</div>}<footer><button type="button" className="secondary-button" onClick={onClose} disabled={busy}>取消</button><button className="primary-button" disabled={!file || busy}>{busy ? <><LoaderCircle size={15} className="spin"/>正在导入</> : '上传资料'}</button></footer></form></section></div>
}
