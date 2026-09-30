import assert from 'node:assert/strict'
import test from 'node:test'
import { tmpdir } from 'node:os'
import { isAbsolute, relative, resolve } from 'node:path'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { withFrontendTestIsolation } from '../src/utils/viteTestIsolation.mjs'

test('browser fixtures isolate HMR and cache while preserving owner configuration', () => {
  const previous = process.env.ADDP_E2E
  const config = { server: { port: 5173, hmr: { port: 5173 }, proxy: { '/api': {} } }, plugins: [{}] }
  let testing
  try {
    delete process.env.ADDP_E2E
    assert.equal(withFrontendTestIsolation('fixture', config), config)
    process.env.ADDP_E2E = '1'
    testing = withFrontendTestIsolation('fixture', config)
    assert.equal(testing.server.hmr, false)
    assert.equal(testing.server.strictPort, true)
    assert.equal(testing.server.proxy, config.server.proxy)
    assert.equal(config.server.hmr.port, 5173)
    assert.equal(testing.plugins[0], config.plugins[0])
    const cachePath = relative(tmpdir(), testing.cacheDir)
    assert.equal(isAbsolute(cachePath), false)
    assert.equal(cachePath.startsWith('..'), false)
    assert.match(testing.cacheDir, new RegExp(`vite-test-${process.pid}$`))
    mkdirSync(testing.cacheDir, { recursive: true })
    writeFileSync(resolve(testing.cacheDir, 'fixture'), 'test')
    testing.plugins.at(-1).closeBundle()
    assert.equal(existsSync(testing.cacheDir), false)
    assert.throws(() => withFrontendTestIsolation('../escape', config), /Invalid frontend module/)
  } finally {
    testing?.plugins.at(-1).closeBundle()
    if (previous === undefined) delete process.env.ADDP_E2E
    else process.env.ADDP_E2E = previous
  }
})

test('normal process exit removes a fixture cache before a server is opened', () => {
  const helper = new URL('../src/utils/viteTestIsolation.mjs', import.meta.url).href
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', `
    import { mkdirSync, writeFileSync } from 'node:fs'
    import { withFrontendTestIsolation } from ${JSON.stringify(helper)}
    const config = withFrontendTestIsolation('exit-fixture', {})
    mkdirSync(config.cacheDir, { recursive: true })
    writeFileSync(config.cacheDir + '/fixture', 'test')
    process.stdout.write(config.cacheDir)
  `], { encoding: 'utf8', env: { ...process.env, ADDP_E2E: '1' } })
  assert.equal(result.status, 0, result.stderr)
  assert.ok(result.stdout)
  assert.equal(existsSync(result.stdout), false)
})

for (const signal of ['SIGTERM', 'SIGINT']) {
  test(`${signal} closes the fixture server and removes its cache`, () => {
    const helper = new URL('../src/utils/viteTestIsolation.mjs', import.meta.url).href
    const result = spawnSync(process.execPath, ['--input-type=module', '-e', `
      import { mkdirSync, writeFileSync } from 'node:fs'
      import { withFrontendTestIsolation } from ${JSON.stringify(helper)}
      const config = withFrontendTestIsolation('signal-fixture', {})
      const plugin = config.plugins.at(-1)
      plugin.configureServer?.({ async close() {
        plugin.closeBundle()
        process.stderr.write('closed')
      } })
      mkdirSync(config.cacheDir, { recursive: true })
      writeFileSync(config.cacheDir + '/fixture', 'test')
      process.stdout.write(config.cacheDir)
      process.kill(process.pid, ${JSON.stringify(signal)})
      setInterval(() => {}, 1000)
    `], { encoding: 'utf8', env: { ...process.env, ADDP_E2E: '1' }, timeout: 5000 })
    try {
      assert.equal(result.status, 0, result.stderr || result.signal)
      assert.equal(result.stderr, 'closed')
      assert.equal(existsSync(result.stdout), false)
    } finally {
      if (result.stdout) rmSync(result.stdout, { recursive: true, force: true })
    }
  })
}
