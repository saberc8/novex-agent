import type * as T from './type'
import http from '@/utils/http'

export type * from './type'

const BASE_URL = '/captcha'

/** @desc 获取图片验证码 */
export function getImageCaptcha() {
  return http.get<T.ImageCaptchaResp>(`${BASE_URL}/image`)
}
