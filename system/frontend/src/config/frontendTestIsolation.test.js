import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { resolveConfig } from 'vite'

const configFile = resolve(process.cwd(), 'vite.config.js')

describe('System frontend test isolation', () => {
  it('applies the shared isolation policy to the actual Vite configuration', async () => {
    const previous = process.env.ADDP_E2E
    let testing
    try {
      delete process.env.ADDP_E2E
      const development = await resolveConfig({ configFile }, 'serve')
      process.env.ADDP_E2E = '1'
      testing = await resolveConfig({ configFile, server: { port: 4173 } }, 'serve')
      expect(testing.cacheDir).not.toBe(development.cacheDir)
      expect(testing.server.port).toBe(4173)
      expect(testing.server.hmr).toBe(false)
      expect(testing.server.strictPort).toBe(true)
    } finally {
      await testing?.plugins.find(plugin => plugin.name === 'addp-frontend-test-isolation')?.closeBundle()
      if (previous === undefined) delete process.env.ADDP_E2E
      else process.env.ADDP_E2E = previous
    }
  })
})
