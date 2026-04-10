import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/dag': resolve(__dirname, '../../common-frontend/dag/src'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  optimizeDeps: {
    include: ['@antv/g6']
  },
  server: {
    port: 5177,
    strictPort: true, // 端口被占用时报错，不自动切换
    host: '0.0.0.0',
    hmr: {
      // 修复 iframe 嵌入时 HMR WebSocket 连接问题
      // 在 iframe 中运行时,强制使用正确的端口
      protocol: 'ws',
      host: 'localhost',
      port: 5177,
      clientPort: 5177
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/orchestrator/'  // 开发模式用 /，生产模式用 /orchestrator/
})
