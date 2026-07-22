import { JSEncrypt } from 'jsencrypt'

const publicKey = import.meta.env.VITE_RSA_PUBLIC_KEY

export function encryptByRsa(txt: string) {
  if (!publicKey) {
    throw new Error('VITE_RSA_PUBLIC_KEY 未配置')
  }
  const encryptor = new JSEncrypt()
  encryptor.setPublicKey(publicKey) // 设置公钥
  return encryptor.encrypt(txt) // 对数据进行加密
}
