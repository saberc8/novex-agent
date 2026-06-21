import http from '@/utils/http'

const BASE_URL = '/user/profile'

/** @desc 上传头像 */
export function uploadAvatar(data: FormData) {
  return http.patch(`${BASE_URL}/avatar`, data)
}

/** @desc 修改用户基本信息 */
export function updateUserBaseInfo(data: { nickname: string, gender: number }) {
  return http.patch(`${BASE_URL}/basic/info`, data)
}

/** @desc 修改密码 */
export function updateUserPassword(data: { oldPassword: string, newPassword: string }) {
  return http.patch(`${BASE_URL}/password`, data)
}
