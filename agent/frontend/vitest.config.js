import { mergeConfig } from 'vite'
import { defineConfig } from 'vitest/config'

import viteConfig from './vite.config.js'

export default mergeConfig(viteConfig, defineConfig({
  test: {
    environment: 'jsdom',
    include: [
      'tests/**/*.test.js',
      '../../common-frontend/agent-ui/tests/**/*.test.js'
    ]
  }
}))
