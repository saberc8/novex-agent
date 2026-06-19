import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd())
  const apiPrefix = env.VITE_API_PREFIX || '/dev-api'
  const apiBaseURL = env.VITE_API_BASE_URL || 'http://localhost:4398'

  return {
    plugins: [react()],
    server: {
      host: true,
      port: Number(env.VITE_PORT) || 5174,
      proxy: {
        [apiPrefix]: {
          target: apiBaseURL,
          changeOrigin: true,
          secure: false,
          rewrite: (path) => path.replace(new RegExp(`^${apiPrefix}`), ''),
        },
      },
    },
    build: {
      outDir: 'dist',
    },
  }
})
