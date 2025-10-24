import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common/frontend')
    }
  },
  server: {
    port: 5173,
    strictPort: true, // 端口被占用时报错，不自动切换
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../../common')
      ]
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
