import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

const BUILD_TYPE = process.env.BUILD_TYPE || 'release'
const OUT_BASE = process.env.OUT_DIR
const IS_E2E = process.env.ADDP_E2E === '1'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios']
  },
  server: {
    port: Number(process.env.CONSOLE_FE_PORT || 5170),
    strictPort: true, // 端口被占用时报错，不自动切换
    hmr: IS_E2E ? false : {
      protocol: 'ws',
      host: 'localhost',
      port: Number(process.env.CONSOLE_FE_PORT || 5170),
      clientPort: Number(process.env.CONSOLE_FE_PORT || 5170)
    },
    proxy: {
      ...(IS_E2E ? {
        '/e2e/service-fixture': { target: 'http://127.0.0.1:4180', changeOrigin: true }
      } : {}),
      '/data-apps': {
        target: `http://localhost:${process.env.WORKBENCH_FE_PORT || 5190}`,
        changeOrigin: true,
        ws: true,
        rewrite: (path) => `/workbench${path}`
      },
      '/workbench': {
        target: `http://localhost:${process.env.WORKBENCH_FE_PORT || 5190}`,
        changeOrigin: true,
        ws: true
      },
      '/portal': {
        target: `http://localhost:${process.env.PORTAL_FE_PORT || 5185}`,
        changeOrigin: true,
        ws: true
      },
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`, // 统一通过 Gateway 访问
        changeOrigin: true
      },
      // 模块健康检查代理（避免开发环境 CORS）
      '/module-health/system':      { target: `http://localhost:${process.env.SYSTEM_BACKEND_PORT || 8180}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/manager':     { target: `http://localhost:${process.env.MANAGER_BACKEND_PORT || 8081}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/meta':        { target: `http://localhost:${process.env.META_BACKEND_PORT || 8082}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/develop':     { target: `http://localhost:${process.env.DEVELOP_BACKEND_PORT || 8185}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/transfer':    { target: `http://localhost:${process.env.TRANSFER_BACKEND_PORT || 8083}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/service':     { target: `http://localhost:${process.env.SERVICE_BACKEND_PORT || 8086}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/orchestrator':{ target: `http://localhost:${process.env.ORCHESTRATOR_BACKEND_PORT || 8084}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/monitor':     { target: `http://localhost:${process.env.MONITOR_BACKEND_PORT || 8100}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/standard':    { target: `http://localhost:${process.env.STANDARD_BACKEND_PORT || 8110}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/model':       { target: `http://localhost:${process.env.MODEL_BACKEND_PORT || 8181}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/portal':      { target: `http://localhost:${process.env.PORTAL_BACKEND_PORT || 8184}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/quality':     { target: `http://localhost:${process.env.QUALITY_BACKEND_PORT || 8182}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/agent':       { target: `http://localhost:${process.env.AGENT_BACKEND_PORT || 8190}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/copilot':     { target: `http://localhost:${process.env.COPILOT_BACKEND_PORT || 8087}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/graph':       { target: `http://localhost:${process.env.GRAPH_BACKEND_PORT || 8186}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/inference':   { target: `http://localhost:${process.env.INFERENCE_BACKEND_PORT || 8191}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/catalog':     { target: `http://localhost:${process.env.CATALOG_BACKEND_PORT || 8192}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/workbench':   { target: `http://localhost:${process.env.WORKBENCH_BACKEND_PORT || 8193}`, rewrite: () => '/health/ready', changeOrigin: true },
      '/module-health/ontology':    { target: `http://localhost:${process.env.ONTOLOGY_BACKEND_PORT || 8195}`, rewrite: () => '/health/ready', changeOrigin: true },
      // Swagger spec 代理（避免 swagger-viewer.html 跨域 fetch）
      '/swagger-spec/system':       { target: `http://localhost:${process.env.SYSTEM_BACKEND_PORT || 8180}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/manager':      { target: `http://localhost:${process.env.MANAGER_BACKEND_PORT || 8081}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/meta':         { target: `http://localhost:${process.env.META_BACKEND_PORT || 8082}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/develop':      { target: `http://localhost:${process.env.DEVELOP_BACKEND_PORT || 8185}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/transfer':     { target: `http://localhost:${process.env.TRANSFER_BACKEND_PORT || 8083}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/service':      { target: `http://localhost:${process.env.SERVICE_BACKEND_PORT || 8086}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/orchestrator': { target: `http://localhost:${process.env.ORCHESTRATOR_BACKEND_PORT || 8084}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/monitor':      { target: `http://localhost:${process.env.MONITOR_BACKEND_PORT || 8100}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/standard':     { target: `http://localhost:${process.env.STANDARD_BACKEND_PORT || 8110}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/model':        { target: `http://localhost:${process.env.MODEL_BACKEND_PORT || 8181}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/portal':       { target: `http://localhost:${process.env.PORTAL_BACKEND_PORT || 8184}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/quality':      { target: `http://localhost:${process.env.QUALITY_BACKEND_PORT || 8182}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/agent':        { target: `http://localhost:${process.env.AGENT_BACKEND_PORT || 8190}`, rewrite: () => '/openapi.json', changeOrigin: true },
      '/swagger-spec/copilot':      { target: `http://localhost:${process.env.COPILOT_BACKEND_PORT || 8087}`, rewrite: () => '/openapi.json', changeOrigin: true },
      '/swagger-spec/graph':        { target: `http://localhost:${process.env.GRAPH_BACKEND_PORT || 8186}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/inference':    { target: `http://localhost:${process.env.INFERENCE_BACKEND_PORT || 8191}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/catalog':      { target: `http://localhost:${process.env.CATALOG_BACKEND_PORT || 8192}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/workbench':    { target: `http://localhost:${process.env.WORKBENCH_BACKEND_PORT || 8193}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
      '/swagger-spec/ontology':     { target: `http://localhost:${process.env.ONTOLOGY_BACKEND_PORT || 8195}`, rewrite: () => '/swagger/doc.json', changeOrigin: true },
    }
  },
  build: {
    outDir: resolve(__dirname, OUT_BASE ? `${OUT_BASE}/${BUILD_TYPE}/frontend/console` : 'dist'),
    sourcemap: BUILD_TYPE === 'debug',
    minify: BUILD_TYPE !== 'debug',
    emptyOutDir: true
  }
})
