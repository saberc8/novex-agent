import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { port: 8312, strictPort: true, proxy: { '/api/novex': { target: 'http://127.0.0.1:8310', changeOrigin: false } } },
  preview: { port: 8312, strictPort: true },
})
