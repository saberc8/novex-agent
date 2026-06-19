import { createProdMockServer } from 'vite-plugin-mock/client'

import area from './area'

export function setupProdMockServer() {
  createProdMockServer([...area])
}
