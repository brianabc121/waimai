import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// In development the API runs on :8080; requests stay same-origin through this proxy.
export default defineConfig({
  plugins: [vue()],
  server: { proxy: { '/api': { target: process.env.API_URL || 'http://localhost:8080', changeOrigin: false } } },
})
