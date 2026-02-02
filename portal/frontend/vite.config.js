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
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
    }
  },
  server: {
    port: 5170,
    strictPort: true, // 端口被占用时报错，不自动切换
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5170,
      clientPort: 5170
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: resolve(__dirname, OUT_BASE ? `${OUT_BASE}/${BUILD_TYPE}/frontend/portal` : 'dist'),
    sourcemap: BUILD_TYPE === 'debug',
    minify: BUILD_TYPE !== 'debug',
    emptyOutDir: true
  }
})
