import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src')
    }
  },
  server: {
    port: 5177,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:8084',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/orchestrator/'  // 开发模式用 /，生产模式用 /orchestrator/
})
