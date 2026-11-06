import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Download, LoaderCircle, Search } from 'lucide-react'
import { api, downloadKnowledge, type Document, type SpreadsheetSearch } from './api'

const pageSize = 20
const cellText = (value: unknown) => value == null ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value)
type Criteria = { sheet: string; query: string; filters: { column: string; op: string; value: string | number }[] }

export default function SpreadsheetViewer({ document }: { document: Document }) {
  const [sheet, setSheet] = useState(document.sheets?.[0]?.name || '')
  const [query, setQuery] = useState('')
  const [column, setColumn] = useState('')
  const [op, setOp] = useState('eq')
  const [value, setValue] = useState('')
  const [criteria, setCriteria] = useState<Criteria>({ sheet, query: '', filters: [] })
  const [offset, setOffset] = useState(0)
  const [result, setResult] = useState<SpreadsheetSearch | null>(null)
  const [loading, setLoading] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [error, setError] = useState('')
  const downloadController = useRef<AbortController | null>(null)
  const selectedSheet = document.sheets?.find(item => item.name === sheet)
  const columns = result?.columns || selectedSheet?.columns || []
  const warnings = [...new Set([...(document.warnings || []), ...(result?.warnings || [])])]

  useEffect(() => {
    let alive = true
    if (!criteria.sheet) return
    setLoading(true); setError(''); setResult(null)
    api<SpreadsheetSearch>(`/knowledge/${document.id}/search`, 'POST', { ...criteria, limit: pageSize, offset })
      .then(data => { if (alive) setResult(data) })
      .catch(error => { if (alive) setError(error instanceof Error ? error.message : '表格检索失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [document.id, criteria, offset])
  useEffect(() => () => downloadController.current?.abort(), [])

  function search(event: FormEvent) {
    event.preventDefault()
    let filterValue: string | number = value
    if (column && ['gte', 'lte'].includes(op)) {
      if (!value.trim() || !Number.isFinite(Number(value))) { setError('大于等于或小于等于筛选需要输入数字。'); return }
      filterValue = Number(value)
    }
    setOffset(0)
    setCriteria({ sheet, query: query.trim(), filters: column ? [{ column, op, value: filterValue }] : [] })
  }
  function changeSheet(next: string) {
    setSheet(next); setColumn(''); setValue(''); setOffset(0)
    setCriteria({ sheet: next, query: query.trim(), filters: [] })
  }
  async function download() {
    const controller = new AbortController()
    downloadController.current = controller
    setDownloading(true); setError('')
    try { await downloadKnowledge(document.id, document.fileName, controller.signal) }
    catch (error) { if (!controller.signal.aborted) setError(error instanceof Error ? error.message : '下载失败') }
    finally { if (!controller.signal.aborted) setDownloading(false) }
  }

  return <div className="spreadsheet-viewer">
    <div className="sheet-meta"><span>{document.sheetCount ?? document.sheets?.length ?? 0} 张工作表 · {document.rowCount?.toLocaleString() ?? '—'} 数据行 · 保留原始文件</span><button className="secondary-button" onClick={() => void download()} disabled={downloading}>{downloading ? <LoaderCircle className="spin" size={15}/> : <Download size={15}/>}下载原文件</button></div>
    {warnings.map(warning => <div key={warning} className="sheet-warning" role="note">{warning}</div>)}
    <form onSubmit={search} className="sheet-search-form">
      <div className="sheet-search-row"><label>工作表<select value={sheet} onChange={event => changeSheet(event.target.value)} disabled={loading}>{document.sheets?.map(item => <option key={item.name} value={item.name}>{item.name} · {item.rowCount.toLocaleString()} 行</option>)}</select></label><label>关键词<input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索表格中的内容"/></label></div>
      <div className="sheet-filter-row"><label>指定列（可选）<select value={column} onChange={event => setColumn(event.target.value)}><option value="">不添加列筛选</option>{selectedSheet?.columns.map(item => <option key={item.key} value={item.key}>{item.key} · {item.label}</option>)}</select></label><label>条件<select value={op} onChange={event => setOp(event.target.value)} disabled={!column}><option value="eq">等于</option><option value="contains">包含</option><option value="gte">大于等于</option><option value="lte">小于等于</option></select></label><label>筛选值<input value={value} onChange={event => setValue(event.target.value)} placeholder="输入筛选值" disabled={!column}/></label><button className="primary-button" disabled={loading || !sheet}>{loading ? <LoaderCircle size={15} className="spin"/> : <Search size={15}/>}检索表格</button></div>
    </form>
    {error && <div className="login-error" role="alert">{error}</div>}
    <div className="sheet-table-wrap" aria-busy={loading}><table className="sheet-table"><caption>{sheet} 检索结果，包含原始 Excel 行号和引用位置</caption><thead><tr><th className="sheet-row-number" scope="col">Excel 行号</th>{columns.map(item => <th key={item.key} scope="col">{item.key} · {item.label}</th>)}<th scope="col">引用位置</th></tr></thead><tbody>{result?.rows.map(row => <tr key={`${row.sheet}:${row.row}`}><th className="sheet-row-number" scope="row">{row.row}</th>{columns.map(item => <td key={item.key} title={cellText(row.cells[item.key])}>{cellText(row.cells[item.key])}</td>)}<td className="sheet-citation">{row.citation}</td></tr>)}{!result?.rows.length && <tr><td colSpan={columns.length + 2} className="sheet-empty">{loading ? '正在检索表格…' : '没有匹配的数据行，可调整关键词或筛选条件。'}</td></tr>}</tbody></table></div>
    <div className="sheet-pagination"><span>{result?.rows.length ? `匹配结果 ${offset + 1}–${offset + result.rows.length}` : '0 条结果'} · 每页 {pageSize} 行</span><div><button className="secondary-button" onClick={() => setOffset(value => Math.max(0, value - pageSize))} disabled={loading || offset === 0}>上一页</button><button className="secondary-button" onClick={() => setOffset(value => value + pageSize)} disabled={loading || !result?.hasMore}>下一页</button></div></div>
  </div>
}
