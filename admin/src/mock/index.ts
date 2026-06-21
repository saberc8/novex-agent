import { createProdMockServer } from 'vite-plugin-mock/client'

export function setupProdMockServer() {
  createProdMockServer([])
}
