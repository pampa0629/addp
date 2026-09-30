import { tmpdir } from 'node:os'
import { resolve, relative, isAbsolute } from 'node:path'
import { describe, expect, it } from 'vitest'
import { resolveConfig } from 'vite'

const configFile = resolve(process.cwd(), 'vite.config.js')

describe('System frontend test isolation', () => {
  it('keeps test dependency caches outside the workspace and separate from development', async () => {
    const development = await resolveConfig({ configFile, mode: 'development' }, 'serve')
    const testing = await resolveConfig({ configFile, mode: 'test', server: { port: 4173 } }, 'serve')
    expect(testing.cacheDir).not.toBe(development.cacheDir)
    const cachePath = relative(tmpdir(), testing.cacheDir)
    expect(isAbsolute(cachePath)).toBe(false)
    expect(cachePath.startsWith('..')).toBe(false)
    expect(testing.server.hmr.port ?? testing.server.port).toBe(4173)
    expect(testing.server.hmr.clientPort ?? testing.server.port).toBe(4173)
  })
})
