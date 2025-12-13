import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import monacoEditorPlugin from 'vite-plugin-monaco-editor'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    vue(),
    monacoEditorPlugin({
      languages: ['sql', 'json', 'javascript', 'typescript']
    })
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    port: 5178,
    proxy: {
      '/api': {
        target: 'http://localhost:8085',
        changeOrigin: true
      }
    }
  },
  base: process.env.NODE_ENV === 'development' ? '/' : '/develop/'
})
