import { mergeConfig } from 'vite'
import { defineConfig } from 'vitest/config'

import viteConfig from './vite.config.js'

export default mergeConfig(viteConfig, defineConfig({
  resolve: { dedupe: ['vue-router'] },
  test: {
    environment: 'jsdom',
    include: [
      'tests/**/*.test.js',
      '../../common-frontend/agent-ui/tests/**/*.test.js'
    ]
  }
}))
