import { withModuleFrontend } from '../../common-frontend/basic/src/utils/moduleFrontend.mjs'
import { withFrontendTestIsolation } from '../../common-frontend/basic/src/utils/viteTestIsolation.mjs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'node:path'

const testing = process.env.ADDP_E2E === '1'
export default defineConfig(withModuleFrontend('ontology', withFrontendTestIsolation('ontology', {
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
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6')
    },
    dedupe: [
      'vue',
      'vue-i18n',
      'element-plus',
      '@element-plus/icons-vue',
      'axios'
    ]
  },
  optimizeDeps: {
    // Prebundle the lazy graph and the controlled Console host before serving Vue.
    // Late discovery would mix optimizer generations in the first iframe host.
    entries: testing ? ['index.html', 'e2e/host.html'] : ['index.html'],
    include: [
      'vue', 'vue-router', 'pinia', 'vue-i18n', 'element-plus', 'element-plus/es',
      'element-plus/es/locale/lang/zh-cn', 'element-plus/es/locale/lang/en',
      '@element-plus/icons-vue', 'axios', '@antv/g6'
    ]
  },
  server: {
    port: Number(process.env.ONTOLOGY_FE_PORT || 5192),
    strictPort: true,
    fs: { allow: [resolve(__dirname, '../..')] },
    proxy: testing
      ? {}
      : { '/api': { target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`, changeOrigin: true } }
  }
})))
