import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5170,
    proxy: {
      '/api/meta': {
        target: 'http://localhost:8082',
        changeOrigin: true
      },
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
