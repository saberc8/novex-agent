export interface User { id: string | number; username: string; nickname: string; tenantId: string; departmentId: string; isAdmin: boolean; globalDataAccess?: boolean; roles: { id: number; name: string; code: string }[]; policyVersion: number }
export interface Runtime { id: string; name: string; available: boolean }
export interface Conversation { id: string; title: string; runtime: string; policyVersion: number; createdAt: string; locked: boolean }
export interface Message { id: string; role: 'user' | 'assistant'; content: string; createdAt: string; runId?: string }
export interface Document { id: string; title: string; fileName: string; scope: string; ownerId: string; departmentId: string; tenantId?: string; createdAt: string; characters: number; markdown?: string; kind?: 'markdown' | 'spreadsheet'; sheetCount?: number; rowCount?: number; sheets?: SpreadsheetSheet[]; warnings?: string[] }
export interface SpreadsheetColumn { key: string; label: string; type?: string }
export interface SpreadsheetSheet { name: string; rowCount: number; columnCount: number; columns: SpreadsheetColumn[] }
export interface SpreadsheetSearch { documentId: string; sheet?: string; columns?: SpreadsheetColumn[]; rows: { sheet: string; row: number; cells: Record<string, unknown>; citation: string }[]; hasMore: boolean; warnings?: string[] }
export class ApiError extends Error { constructor(message: string, public status: number) { super(message) } }

const base = import.meta.env.VITE_API_BASE || '/api/novex'
let token = ''
export function setToken(value: string) { token = value }
export async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const form = body instanceof FormData
  const response = await fetch(`${base}${path}`, { method, headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(!form && body !== undefined ? { 'Content-Type': 'application/json' } : {}) }, body: body === undefined ? undefined : form ? body : JSON.stringify(body) })
  const result = await response.json().catch(() => null)
  if (!response.ok || !result?.success) throw new ApiError(result?.msg || `请求失败 (${response.status})`, response.status)
  return result.data as T
}
export async function streamRun(runId: string, signal: AbortSignal, onEvent: (event: string, data: Record<string, unknown>) => void) {
  const response = await fetch(`${base}/runs/${encodeURIComponent(runId)}/events`, { headers: { Authorization: `Bearer ${token}` }, signal })
  if (!response.ok) { const data = await response.json().catch(() => null); throw new ApiError(data?.msg || '无法连接运行状态', response.status) }
  if (!response.body) throw new Error('浏览器不支持流式响应')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let pending = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      pending = (pending + decoder.decode(value, { stream: !done })).replace(/\r\n/g, '\n')
      let boundary: number
      while ((boundary = pending.indexOf('\n\n')) >= 0) {
        const block = pending.slice(0, boundary)
        pending = pending.slice(boundary + 2)
        const lines = block.split('\n')
        const name = lines.find(line => line.startsWith('event:'))?.slice(6).trim() || 'message'
        const json = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
        if (json) onEvent(name, JSON.parse(json))
      }
      if (done) break
    }
  } finally { reader.releaseLock() }
}

export async function downloadKnowledge(id: string, fileName: string, signal?: AbortSignal) {
  const requestToken = token
  const response = await fetch(`${base}/knowledge/${encodeURIComponent(id)}/file`, { headers: { Authorization: `Bearer ${requestToken}` }, signal })
  if (!response.ok) {
    const result = await response.json().catch(() => null)
    throw new ApiError(result?.msg || `下载失败 (${response.status})`, response.status)
  }
  const blob = await response.blob()
  if (signal?.aborted || token !== requestToken) return
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName.split(/[\\/]/).pop() || 'knowledge.xlsx'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
