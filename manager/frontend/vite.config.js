import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import { fileURLToPath } from 'url'
import { viteStaticCopy } from 'vite-plugin-static-copy'

const __dirname = fileURLToPath(new URL('.', import.meta.url))

export default defineConfig({
  plugins: [
    vue(),
    viteStaticCopy({
      targets: [
        { src: 'node_modules/cesium/Build/Cesium/Workers', dest: 'cesium' },
        { src: 'node_modules/cesium/Build/Cesium/Assets', dest: 'cesium' },
        { src: 'node_modules/cesium/Build/Cesium/ThirdParty', dest: 'cesium' },
        { src: 'node_modules/cesium/Build/Cesium/Widgets', dest: 'cesium' },
        { src: 'node_modules/@dfsj/s3m/lib/draco_decoder_new.wasm', dest: 'S3M_module/S3MParser' },
        { src: 'node_modules/@dfsj/s3m/lib/crunch.wasm', dest: 'S3M_module/S3MTiles/ThirdParty' },
        { src: 'node_modules/@mlightcad/libredwg-converter/dist/libredwg-parser-worker.js', dest: 'cad-engine' },
        { src: 'node_modules/@mlightcad/libredwg-converter/dist/libredwg-web.wasm', dest: 'cad-engine' },
        { src: 'node_modules/@mlightcad/cad-simple-viewer/dist/mtext-renderer-worker.js', dest: 'cad-engine' },
        { src: 'LICENSE', dest: 'licenses', rename: 'manager-frontend-GPL-3.0.txt' },
        { src: 'THIRD_PARTY_NOTICES.md', dest: 'licenses' },
        { src: 'SOURCE_OFFER.md', dest: 'licenses' }
      ]
    })
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src'),
      '@addp/common-frontend/graph': resolve(__dirname, '../../common-frontend/graph/src'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'geotiff': resolve(__dirname, 'node_modules/geotiff'),
      'mermaid': resolve(__dirname, 'node_modules/mermaid'),
      'proj4': resolve(__dirname, 'node_modules/proj4')
    },
    dedupe: ['ol', 'vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'marked', 'mammoth', 'dompurify', 'jszip', 'mermaid', 'geotiff', 'proj4', 'axios', '@amap/amap-jsapi-loader', '@antv/g6']
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
      'ol/source/GeoTIFF',
      'geotiff',
      'ol/style/Style',
      'ol/style/Fill',
      'ol/style/Stroke',
      'ol/style/Circle',
      'ol/Overlay',
      'ol/interaction',
      'ol/control',
      '@amap/amap-jsapi-loader',
      'proj4'
    ]
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/@mlightcad/')) return 'mlightcad'
        }
      }
    }
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
    },
    fs: {
      allow: ['..']
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/manager/'  // 开发模式用 /，生产模式用 /manager/
})
