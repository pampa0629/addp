import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'path'

const ENTRY_CHUNK_LIMIT_BYTES = 500 * 1024
const isE2E = process.env.ADDP_E2E === '1'

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
  cacheDir: isE2E ? 'node_modules/.vite-e2e' : 'node_modules/.vite',
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver({ importStyle: false })] }),
    enforceEntryChunkBudget()
  ],
  resolve: { alias: {
    '@': resolve(__dirname, 'src'), '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
    '@common-ui-chart': resolve(__dirname, '../../common-frontend/chart/src'),
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src'),
    '@amap/amap-jsapi-loader': resolve(__dirname, 'node_modules/@amap/amap-jsapi-loader'),
    '@element-plus/icons-vue': resolve(__dirname, 'node_modules/@element-plus/icons-vue'),
    'element-plus': resolve(__dirname, 'node_modules/element-plus'), 'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
  }, dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', 'echarts', 'ol', 'proj4'] },
  server: { port: 5190, strictPort: true, hmr: !isE2E, fs: { allow: [resolve(__dirname, '..'), resolve(__dirname, '../..'), resolve(__dirname, '../../common-frontend')] }, proxy: isE2E ? {} : { '/api': { target: 'http://localhost:8000', changeOrigin: true } } },
  base: isE2E ? '/' : '/workbench/'
})
