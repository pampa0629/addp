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
    },
    dedupe: ['vue', 'element-plus', '@element-plus/icons-vue', 'axios']
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
      },
      // 模块健康检查代理（避免开发环境 CORS）
      '/module-health/system':      { target: 'http://localhost:8180', rewrite: () => '/health', changeOrigin: true },
      '/module-health/manager':     { target: 'http://localhost:8081', rewrite: () => '/health', changeOrigin: true },
      '/module-health/meta':        { target: 'http://localhost:8082', rewrite: () => '/health', changeOrigin: true },
      '/module-health/develop':     { target: 'http://localhost:8185', rewrite: () => '/health', changeOrigin: true },
      '/module-health/transfer':    { target: 'http://localhost:8083', rewrite: () => '/health', changeOrigin: true },
      '/module-health/service':     { target: 'http://localhost:8086', rewrite: () => '/health', changeOrigin: true },
      '/module-health/orchestrator':{ target: 'http://localhost:8084', rewrite: () => '/health', changeOrigin: true },
      '/module-health/monitor':     { target: 'http://localhost:8100', rewrite: () => '/health', changeOrigin: true },
      '/module-health/standard':    { target: 'http://localhost:8110', rewrite: () => '/health', changeOrigin: true },
      '/module-health/model':       { target: 'http://localhost:8181', rewrite: () => '/health', changeOrigin: true },
      '/module-health/agent':       { target: 'http://localhost:8190', rewrite: () => '/health', changeOrigin: true },
      '/module-health/copilot':     { target: 'http://localhost:8087', rewrite: () => '/health', changeOrigin: true },
      '/module-health/graph':       { target: 'http://localhost:8186', rewrite: () => '/health', changeOrigin: true },
      // Swagger spec 代理（避免 swagger-viewer.html 跨域 fetch）
      '/swagger-spec/system':       { target: 'http://localhost:8180', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/manager':      { target: 'http://localhost:8081', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/meta':         { target: 'http://localhost:8082', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/develop':      { target: 'http://localhost:8185', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/transfer':     { target: 'http://localhost:8083', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/service':      { target: 'http://localhost:8086', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/orchestrator': { target: 'http://localhost:8084', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/monitor':      { target: 'http://localhost:8100', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/standard':     { target: 'http://localhost:8110', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/model':        { target: 'http://localhost:8181', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/portal':       { target: 'http://localhost:8184', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/quality':      { target: 'http://localhost:8182', rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/agent':        { target: 'http://localhost:8190', rewrite: () => '/openapi.json', changeOrigin: true },
      '/swagger-spec/copilot':      { target: 'http://localhost:8087', rewrite: () => '/openapi.json', changeOrigin: true },
      '/swagger-spec/graph':        { target: 'http://localhost:8186', rewrite: () => '/swagger/doc.json', changeOrigin: true },
    }
  },
  build: {
    outDir: resolve(__dirname, OUT_BASE ? `${OUT_BASE}/${BUILD_TYPE}/frontend/console` : 'dist'),
    sourcemap: BUILD_TYPE === 'debug',
    minify: BUILD_TYPE !== 'debug',
    emptyOutDir: true
  }
})
