import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'node:path'

const testing = process.env.ADDP_E2E === '1'
export default defineConfig({
  base: '/ontology/',
  cacheDir: testing ? 'node_modules/.vite-e2e' : 'node_modules/.vite',
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver({ importStyle: false })] })
  ],
  resolve: {
    alias: {
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      'element-plus': resolve(__dirname, 'node_modules/element-plus'),
      '@element-plus/icons-vue': resolve(
        __dirname,
        'node_modules/@element-plus/icons-vue'
      ),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: [
      'vue',
      'vue-i18n',
      'element-plus',
      '@element-plus/icons-vue',
      'axios'
    ]
  },
  server: {
    port: 5192,
    strictPort: true,
    hmr: !testing,
    fs: { allow: [resolve(__dirname, '../..')] },
    proxy: testing
      ? {}
      : { '/api': { target: 'http://localhost:8000', changeOrigin: true } }
  }
})
