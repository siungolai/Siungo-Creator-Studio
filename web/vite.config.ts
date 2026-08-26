import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// 单二进制部署：构建产物经 scripts/sync-static.mjs 同步到 server/static（go:embed 目标）。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8081',
    },
  },
})
