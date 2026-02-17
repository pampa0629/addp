import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
    },
    dedupe: ['ol', 'vue']
  },
  optimizeDeps: {
    include: [
      'ol',
      'ol/layer/Tile',
      'ol/layer/VectorTile',
      'ol/layer/Vector',
      'ol/source/XYZ',
      'ol/source/VectorTile',
      'ol/source/Vector',
      'ol/format/MVT',
      'ol/format/GeoJSON',
      'ol/style/Style',
      'ol/style/Fill',
      'ol/style/Stroke',
      'ol/style/Circle',
      'ol/Overlay',
      'ol/interaction',
      'ol/control'
    ]
  },
  server: {
    port: 5174,
    strictPort: true, // 端口被占用时报错，不自动切换
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5174,
      clientPort: 5174
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/manager/'  // 开发模式用 /，生产模式用 /manager/
})