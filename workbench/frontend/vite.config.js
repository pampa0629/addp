import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
export default defineConfig({
  plugins: [vue()],
  resolve: { alias: {
    '@': resolve(__dirname, 'src'), '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
    '@common-ui-chart': resolve(__dirname, '../../common-frontend/chart/src'),
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src'),
    '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
    'element-plus': resolve(__dirname, 'node_modules/element-plus'), 'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
  }, dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', 'echarts', 'ol', 'proj4'] },
  server: { port: 5190, strictPort: true, fs: { allow: [resolve(__dirname, '..'), resolve(__dirname, '../..'), resolve(__dirname, '../../common-frontend')] }, proxy: { '/api': { target: 'http://localhost:8000', changeOrigin: true } } },
  base: process.env.NODE_ENV === 'development' ? '/' : '/workbench/'
})
