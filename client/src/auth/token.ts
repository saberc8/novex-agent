const TOKEN_KEY = 'client_token'

export function getClientToken() {
  return window.localStorage.getItem(TOKEN_KEY) || ''
}

export function setClientToken(token: string) {
  window.localStorage.setItem(TOKEN_KEY, token)
}

export function clearClientToken() {
  window.localStorage.removeItem(TOKEN_KEY)
}
