import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development the React app proxies API and WebSocket traffic to the Go server,
// so the browser sees one origin and no CORS setup is needed locally.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
})
