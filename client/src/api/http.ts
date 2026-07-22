import axios from 'axios'
import { clearClientToken, getClientToken } from '../auth/token'

export interface ApiResponse<T> {
  code: string
  data: T
  msg: string
  success: boolean
  timestamp: string
}

const apiPrefix = import.meta.env.VITE_API_PREFIX || '/dev-api'

export const http = axios.create({
  baseURL: apiPrefix,
  timeout: 12000,
})

http.interceptors.request.use((config) => {
  const token = getClientToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (response) => {
    const body = response.data as ApiResponse<unknown>
    if (body?.code === '401') {
      clearClientToken()
    }
    if (body?.code && body.code !== '200') {
      return Promise.reject(new Error(body.msg || '请求失败'))
    }
    return response
  },
  (error) => {
    const body = error.response?.data as Partial<ApiResponse<unknown>> | undefined
    if (error.response?.status === 401 || body?.code === '401') {
      clearClientToken()
    }
    return Promise.reject(new Error(body?.msg || error.message || '请求失败'))
  },
)

export async function post<T, D = unknown>(url: string, data?: D) {
  const response = await http.post<ApiResponse<T>>(url, data)
  return response.data.data
}

export async function get<T>(url: string) {
  const response = await http.get<ApiResponse<T>>(url)
  return response.data.data
}
