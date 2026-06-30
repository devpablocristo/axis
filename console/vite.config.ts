import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const bffTarget = process.env.AXIS_BFF_BASE_URL || 'http://localhost:18080'
const hmrClientPort = Number.parseInt(process.env.VITE_HMR_CLIENT_PORT || '', 10)

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    hmr: Number.isFinite(hmrClientPort) ? { clientPort: hmrClientPort } : undefined,
    proxy: {
      '/api': bffTarget,
      '/healthz': bffTarget,
      '/readyz': bffTarget
    }
  }
})
