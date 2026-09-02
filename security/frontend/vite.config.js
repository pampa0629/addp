import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue(), Components({ resolvers: [ElementPlusResolver({ importStyle: false })] })],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: { port: 5191, strictPort: true, fs: { allow: [resolve(__dirname, '..'), resolve(__dirname, '../..'), resolve(__dirname, '../../common-frontend')] }, proxy: { '/api': { target: 'http://localhost:8000', changeOrigin: true } } },
  base: process.env.NODE_ENV === 'development' ? '/' : '/security/'
})
