import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import path from 'path'
import { resolve } from 'path'

const isE2E = process.env.ADDP_E2E === '1'
const ENTRY_CHUNK_LIMIT_BYTES = 500 * 1024

const enforceEntryChunkBudget = () => ({
  name: 'enforce-entry-chunk-budget',
  generateBundle(_, bundle) {
    for (const chunk of Object.values(bundle)) {
      if (chunk.type !== 'chunk' || !chunk.isEntry) continue
      const bytes = Buffer.byteLength(chunk.code, 'utf8')
      if (bytes > ENTRY_CHUNK_LIMIT_BYTES) {
        this.error(`${chunk.fileName} is ${Math.ceil(bytes / 1024)} KiB; entry chunks must stay within 500 KiB`)
      }
    }
  }
})

export default defineConfig({
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver({ importStyle: false })] }),
    enforceEntryChunkBudget()
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
    port: 5182,
    strictPort: true,
    hmr: isE2E ? false : {
      protocol: 'ws',
      host: 'localhost',
      port: 5182,
      clientPort: 5182
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
        target: 'http://localhost:8000',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/model/',
  build: {
    // Mermaid keeps unused diagram engines in lazy chunks; the entry budget above
    // remains the user-facing performance gate.
    chunkSizeWarningLimit: 1500,
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
