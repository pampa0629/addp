import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

const BUILD_TYPE = process.env.BUILD_TYPE || 'release'
const OUT_BASE = process.env.OUT_DIR

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      // 当 common-frontend 导入 element-plus 相关库时，使用当前项目的依赖
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  optimizeDeps: {
    exclude: [],
    include: ['@element-plus/icons-vue']  // 明确包含这个依赖
  },
  server: {
    port: 5173,
    strictPort: true, // 端口被占用时报错，不自动切换
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5173,
      clientPort: 5173
    },
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../../common'),
        resolve(__dirname, '../../common-frontend'),
        resolve(__dirname, '../../manager'),
        resolve(__dirname, '../../meta'),
        resolve(__dirname, '../../transfer'),
        resolve(__dirname, '../../develop'),
        resolve(__dirname, '../../service'),
        resolve(__dirname, '../../orchestrator'),
        resolve(__dirname, '../../gateway')
      ]
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/system/',  // 开发模式用 /，生产模式用 /system/
  build: {
    outDir: resolve(__dirname, OUT_BASE ? `${OUT_BASE}/${BUILD_TYPE}/frontend/system` : 'dist'),
    sourcemap: BUILD_TYPE === 'debug',
    minify: BUILD_TYPE !== 'debug',
    emptyOutDir: true,
    rollupOptions: {
      // 确保 @element-plus/icons-vue 被正确解析
      external: [],
      output: {
        manualChunks: {
          'element-plus': ['element-plus', '@element-plus/icons-vue']
        }
      }
    }
  }
})
