import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

const BUILD_TYPE = process.env.BUILD_TYPE || 'release'
const OUT_BASE = process.env.OUT_DIR

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/dag': resolve(__dirname, '../../common-frontend/dag/src'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  optimizeDeps: {
    include: ['@antv/g6']
  },
  server: {
    port: 5186,
    strictPort: true,
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5186,
      clientPort: 5186
    },
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../../common-frontend'),
      ]
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/agent/',
  build: {
    outDir: resolve(__dirname, OUT_BASE ? `${OUT_BASE}/${BUILD_TYPE}/frontend/agent` : 'dist'),
    sourcemap: BUILD_TYPE === 'debug',
    minify: BUILD_TYPE !== 'debug',
    emptyOutDir: true,
  }
})
