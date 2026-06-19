import { get, post } from './http'

export interface ClientLoginReq {
  phone: string
  password: string
  clientId: string
}

export interface ClientRegisterReq extends ClientLoginReq {
  nickname: string
}

export interface ClientLoginResp {
  token: string
  userId: number
  phone: string
  nickname: string
  avatar: string
}

export interface ClientProfile {
  id: number
  phone: string
  nickname: string
  avatar: string
}

export function registerClientUser(data: ClientRegisterReq) {
  return post<ClientLoginResp, ClientRegisterReq>('/client/auth/register', data)
}

export function loginClientUser(data: ClientLoginReq) {
  return post<ClientLoginResp, ClientLoginReq>('/client/auth/login', data)
}

export function logoutClientUser() {
  return post<boolean>('/client/auth/logout')
}

export function getClientProfile() {
  return get<ClientProfile>('/client/user/profile')
}
