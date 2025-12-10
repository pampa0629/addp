import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src')
    }
  },
  server: {
    port: 5174,
    strictPort: true, // 端口被占用时报错，不自动切换
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/manager/'  // 开发模式用 /，生产模式用 /manager/
})