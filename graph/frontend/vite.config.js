import { withModuleFrontend } from '../../common-frontend/basic/src/utils/moduleFrontend.mjs'
import { withFrontendTestIsolation } from '../../common-frontend/basic/src/utils/viteTestIsolation.mjs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import { readFileSync } from 'node:fs'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig(withModuleFrontend('graph', withFrontendTestIsolation('graph', {
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver({ importStyle: false })] }),
    {
      name: 'graph-entry-load-boundary',
      generateBundle(_, bundle) {
        const pending = Object.values(bundle).filter(item => item.type === 'chunk' && (
          item.isEntry || /\/src\/views\/(OntologyList|OntologyForm|OntologyDetail|KnowledgeGraphList)\.vue$/.test(item.facadeModuleId || '')
        ))
        const visited = new Set()
        while (pending.length) {
          const chunk = pending.pop()
          if (!chunk || chunk.type !== 'chunk' || visited.has(chunk.fileName)) continue
          visited.add(chunk.fileName)
          if (Object.keys(chunk.modules).some(id => /\/node_modules\/@antv\/g6(?:[/-])/.test(id))) {
            this.error(`Graph entry and definition pages must load G6 only through a graph view: ${chunk.fileName}`)
          }
          chunk.imports.forEach(file => pending.push(bundle[file]))
        }
      }
    }
  ],
  // Lazy component auto-imports must not replace the optimizer generation while
  // a module navigation is being synchronized with Console.
  optimizeDeps: {
    noDiscovery: true,
    include: [
      ...Object.keys(JSON.parse(readFileSync(resolve(__dirname, 'package.json'), 'utf8')).dependencies),
      'element-plus/es'
    ]
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
      '@addp/common-frontend/graph': resolve(__dirname, '../../common-frontend/graph/src'),
      '@antv/g6': resolve(__dirname, 'node_modules/@antv/g6'),
      'vue-i18n': resolve(__dirname, 'node_modules/vue-i18n')
    },
    dedupe: ['vue', 'vue-i18n', 'element-plus', '@element-plus/icons-vue', 'axios', '@antv/g6']
  },
  server: {
    port: Number(process.env.GRAPH_FE_PORT || 5187),
    strictPort: true,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || 8000}`,
        changeOrigin: true
      }
    }
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/@antv/')) return 'graph-vendor'
          if (id.includes('/node_modules/element-plus/')) return 'element-vendor'
        }
      }
    }
  },
})))
