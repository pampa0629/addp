import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    vue()
  ],

  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/dag': resolve(__dirname, '../../common-frontend/dag/src'),
      '@addp/common-frontend/graph': resolve(__dirname, '../../common-frontend/graph/src'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },

  optimizeDeps: {
    exclude: ['monaco-editor'],
    include: [
      '@antv/g6',
      'sql-formatter',
      '@element-plus/icons-vue'
    ]
  },

  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          'monaco': ['monaco-editor'],
          'graph': ['@antv/g6'],
          'element-plus': ['element-plus', '@element-plus/icons-vue']
        }
      }
    }
  },

  server: {
    port: 5178,
    strictPort: true,
    host: '0.0.0.0',
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5178,
      clientPort: 5178
    },
    fs: {
      allow: ['..']
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000',  // 代理到 Gateway，由 Gateway 统一路由
        changeOrigin: true
      }
    }
  },

  base: process.env.NODE_ENV === 'development' ? '/' : '/develop/'
})
