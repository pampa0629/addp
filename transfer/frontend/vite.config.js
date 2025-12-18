import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5176,
    strictPort: true, // 端口被占用时报错，不自动切换
    hmr: {
      protocol: 'ws',
      host: 'localhost',
      port: 5176,
      clientPort: 5176
    },
    fs: {
      allow: [
        resolve(__dirname, '..'),
        resolve(__dirname, '../../common'),
        resolve(__dirname, '../../common-frontend')
      ]
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8000', // 统一通过 Gateway 访问
        changeOrigin: true
      }
    }
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      // 当 common-frontend 导入 element-plus 相关库时，使用当前项目的依赖
      '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus')
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/transfer/'  // 开发模式用 /，生产模式用 /transfer/
})
