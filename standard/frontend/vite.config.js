import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import path from 'path'
import { resolve } from 'path'

export default defineConfig({
  // Browser gates must not replace dependency chunks used by the live dev server.
  cacheDir: process.env.ADDP_E2E === '1' ? 'node_modules/.vite-e2e' : 'node_modules/.vite',
  optimizeDeps: { include: ['element-plus/es'] },
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver({ importStyle: false })] })
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/basic': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend': resolve(__dirname, '../../common-frontend'),
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: {
    port: Number(process.env.STANDARD_FE_PORT || 5181),
    strictPort: true,
    // E2E uses port 4181 and must not bind to or reload from the live server.
    hmr: process.env.ADDP_E2E === '1' ? false : {
      protocol: 'ws',
      host: 'localhost',
      port: Number(process.env.STANDARD_FE_PORT || 5181),
      clientPort: Number(process.env.STANDARD_FE_PORT || 5181)
    },
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
  base: process.env.NODE_ENV === 'development' ? '/' : '/standard/',
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/@element-plus/icons-vue/')) return 'element-icons'
          if (
            id.includes('/node_modules/vue/') ||
            id.includes('/node_modules/vue-router/') ||
            id.includes('/node_modules/pinia/') ||
            id.includes('/node_modules/vue-i18n/')
          ) return 'vue-vendor'
        }
      }
    }
  }
})
