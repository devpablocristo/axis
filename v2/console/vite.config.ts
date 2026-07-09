import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const bffTarget = process.env.AXIS_BFF_BASE_URL || 'http://localhost:19080'
const hmrClientPort = Number.parseInt(process.env.VITE_HMR_CLIENT_PORT || '', 10)
const serverPort = Number.parseInt(process.env.AXIS_CONSOLE_INTERNAL_PORT || process.env.AXIS_CONSOLE_PORT || '', 10)

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: Number.isFinite(serverPort) ? serverPort : 19173,
    allowedHosts: ['localhost', '127.0.0.1', 'console-v2', 'console-v2-e2e'],
    hmr: Number.isFinite(hmrClientPort) ? { clientPort: hmrClientPort } : undefined,
    proxy: {
      '/api': bffTarget,
      '/healthz': bffTarget,
      '/readyz': bffTarget
    }
  }
})
