import type * as T from './type'
import http from '@/utils/http'

export type * from './type'

const BASE_URL = '/auth'

/** @desc 账号登录 */
export function accountLogin(req: T.AccountLoginReq) {
  return http.post<T.LoginResp>(`${BASE_URL}/login`, req)
}

/** @desc 退出登录 */
export function logout() {
  return http.post(`${BASE_URL}/logout`)
}

/** @desc 获取用户信息 */
export const getUserInfo = () => {
  return http.get<T.UserInfo>(`${BASE_URL}/user/info`)
}

/** @desc 获取路由信息 */
export const getUserRoute = () => {
  return http.get<T.RouteItem[]>(`${BASE_URL}/user/route`)
}
