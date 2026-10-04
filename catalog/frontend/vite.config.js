import { withModuleFrontend } from '../../common-frontend/basic/src/utils/moduleFrontend.mjs'
import { withFrontendTestIsolation } from '../../common-frontend/basic/src/utils/viteTestIsolation.mjs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig(withModuleFrontend('catalog', withFrontendTestIsolation('catalog', {
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/basic': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/graph': resolve(__dirname, '../../common-frontend/graph/src'),
      '@addp/common-frontend': resolve(__dirname, '../../common-frontend'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  server: {
    port: Number(process.env.CATALOG_FE_PORT || 5189),
    strictPort: true,
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../..'),
        resolve(__dirname, '../../common-frontend')
      ]
    },
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`,
        changeOrigin: true
      }
    }
  },
})))
