import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { source } from './scripts/i18n.mjs'

// virtual:i18n-source is every interface text with its Chinese and English,
// read from the code the way `npm run i18n source` does; only the platform's
// language page loads it.
function i18nSource() {
  const id = 'virtual:i18n-source'
  return {
    name: 'i18n-source',
    resolveId: (s: string) => (s === id ? '\0' + id : null),
    load: (s: string) => (s === '\0' + id ? `export default ${JSON.stringify(source())}` : null),
  }
}

const api = process.env.API_URL || 'http://localhost:8080'
// The chat server (OpenIM), reached under /im as in production (deploy/nginx.conf).
const imApi = process.env.OPENIM_API || 'http://localhost:10002'
const imWs = process.env.OPENIM_WS || 'ws://localhost:10001'

// In development the API runs on :8080; requests stay same-origin through this proxy.
export default defineConfig({
  plugins: [vue(), i18nSource()],
  resolve: {
    alias: {
      // see src/openim-pullorder.ts
      '@openim/protocol/lib/pb/sdkws/sdkws': fileURLToPath(new URL('./src/openim-pullorder.ts', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': { target: api, changeOrigin: false },
      '/im/api': { target: imApi, changeOrigin: true, rewrite: p => p.replace(/^\/im\/api/, '') },
      '/im/ws': { target: imWs, ws: true, changeOrigin: true, rewrite: p => p.replace(/^\/im\/ws/, '') },
    },
  },
})
