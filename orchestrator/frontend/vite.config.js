import { withModuleFrontend } from '../../common-frontend/basic/src/utils/moduleFrontend.mjs'
import { withFrontendTestIsolation } from '../../common-frontend/basic/src/utils/viteTestIsolation.mjs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig(withModuleFrontend('orchestrator', withFrontendTestIsolation('orchestrator', {
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/dag': resolve(__dirname, '../../common-frontend/dag/src'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  optimizeDeps: {
    include: ['@antv/g6']
  },
  server: {
    port: Number(process.env.ORCHESTRATOR_FE_PORT || 5177),
    strictPort: true, // 端口被占用时报错，不自动切换
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`, // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
})))
