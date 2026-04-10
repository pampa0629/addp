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
      // 当 common-frontend 导入 element-plus 相关库时，使用当前项目的依赖
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: {
    port: 5179,
    strictPort: true,
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5179,
      clientPort: 5179
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
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/monitor/'
})
