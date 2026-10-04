import { withModuleFrontend } from '../../common-frontend/basic/src/utils/moduleFrontend.mjs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig(withModuleFrontend('meta', {
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: {
    port: Number(process.env.META_FE_PORT || 5175),
    strictPort: true, // 端口被占用时报错，不自动切换
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`, // 统一通过 Gateway 访问
        changeOrigin: true
      }
    },
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../../common-frontend')
      ]
    }
  }
}))
