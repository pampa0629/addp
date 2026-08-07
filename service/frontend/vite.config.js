import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src'),
      '@common-ui-graph': resolve(__dirname, '../../common-frontend/graph/src'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      'proj4': resolve(__dirname, 'node_modules/proj4'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
  dedupe: ['ol', 'proj4', 'vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  optimizeDeps: {
    include: ['ol', 'proj4']
  },
  server: {
    port: 5180,
    strictPort: true,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true
      },
      '/ogc': {
        target: 'http://localhost:8000',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/service/'
})
