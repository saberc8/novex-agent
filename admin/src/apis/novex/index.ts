import { getToken } from '@/utils/auth'

export interface Datasource {
  id: string
  name: string
  host: string
  port: number
  database: string
  username: string
  sslMode: string
  tenantId?: string
  hasPassword?: boolean
  createdAt?: string
}
export interface Column {
  name: string
  dataType: string
  nullable: boolean
}
export interface SchemaTable {
  schema: string
  name: string
  columns: Column[]
}
export interface Resource {
  id: string
  name: string
  key: string
  datasourceId: string
  schema: string
  table: string
  tenantColumn: string
  ownerColumn?: string
  departmentColumn?: string
  warehouseColumn?: string
  identityRequired: boolean
  columns: string[]
}
export interface Identity {
  id: string
  userId: number
  datasourceId: string
  tenantId: string
  employeeId: string
  externalTenantId: string
  departmentId: string
  warehouseIds: string[]
}
export interface Role {
  id: number
  name: string
  code: string
}
export interface NovexUser {
  id: number
  username: string
  nickname: string
  novexEnabled: boolean
  systemDeptId: number
  tenantId: string
  departmentId: string
  globalDataAccess?: boolean
  roles: Role[]
}
export interface Grant {
  id: string
  roleId: number
  resourceId: string
  scope: string
  fields: Record<string, string>
}
export interface Knowledge {
  id: string
  title: string
  fileName: string
  scope: string
  tenantId?: string
  ownerId: string
  departmentId: string
  characters: number
  createdAt: string
  markdown?: string
  kind?: 'markdown' | 'spreadsheet'
  sheetCount?: number
  rowCount?: number
  sheets?: SpreadsheetSheet[]
  warnings?: string[]
}
export interface SpreadsheetColumn {
  key: string
  label: string
  type?: string
}
export interface SpreadsheetSheet {
  name: string
  rowCount: number
  columnCount: number
  columns: SpreadsheetColumn[]
}
export interface SpreadsheetSearch {
  documentId: string
  sheet?: string
  columns?: SpreadsheetColumn[]
  rows: { sheet: string, row: number, cells: Record<string, unknown>, citation: string }[]
  hasMore: boolean
  warnings?: string[]
}
export interface Audit {
  id: string
  userId: number
  action: string
  resourceId: string
  allowed: boolean
  detail: string | object
  createdAt: string
}
export interface Preview {
  rows: Record<string, unknown>[]
  columns: string[]
  resource: { id: string, key: string, name: string }
  policyVersion: number
  explanation: string | object
}
export async function novex<T>(
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<T> {
  const form = body instanceof FormData
  const token = getToken()
  const response = await fetch(
    `${import.meta.env.VITE_API_PREFIX || ''}/api/novex${path}`,
    {
      method,
      headers: {
        Authorization: `Bearer ${token || ''}`,
        ...(!form && body !== undefined
          ? { 'Content-Type': 'application/json' }
          : {}),
      },
      body: body === undefined ? undefined : form ? body : JSON.stringify(body),
    },
  )
  const result = await response.json().catch(() => null)
  if (!response.ok || !result?.success)
    throw new Error(result?.msg || `请求失败 (${response.status})`)
  return result.data as T
}

export async function downloadKnowledge(id: string, fileName: string, signal?: AbortSignal) {
  const token = getToken()
  const response = await fetch(`${import.meta.env.VITE_API_PREFIX || ''}/api/novex/knowledge/${encodeURIComponent(id)}/file`, {
    headers: { Authorization: `Bearer ${token || ''}` },
    signal,
  })
  if (!response.ok) {
    const result = await response.json().catch(() => null)
    throw new Error(result?.msg || `下载失败 (${response.status})`)
  }
  const blob = await response.blob()
  if (signal?.aborted || getToken() !== token) return
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName.split(/[\\/]/).pop() || 'knowledge.xlsx'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
