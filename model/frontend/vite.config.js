import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/basic': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend': resolve(__dirname, '../../common-frontend'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus')
    },
    dedupe: ['vue', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: {
    port: 5182,
    strictPort: true,
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5182,
      clientPort: 5182
    },
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../..'),
        resolve(__dirname, '../../common-frontend')
      ]
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/model/'
})
