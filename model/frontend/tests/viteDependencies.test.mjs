import test from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { loadConfigFromFile } from 'vite'

test('development prebundles auto-imported Element Plus before serving the app', async () => {
  const previous = process.env.ADDP_E2E
  delete process.env.ADDP_E2E
  try {
    const loaded = await loadConfigFromFile(
      { command: 'serve', mode: 'development' },
      fileURLToPath(new URL('../vite.config.js', import.meta.url))
    )
    const dependencies = loaded.config.optimizeDeps?.include || []
    assert.ok(dependencies.includes('element-plus/es'), 'auto-imported Element Plus must not wait for runtime discovery')
    assert.ok(dependencies.includes('element-plus/es/components/table/index'), 'lazy pages must not replace the initial optimized dependency graph')
    assert.ok(dependencies.includes('vue'), 'the initial graph must include the shared Vue runtime')
  } finally {
    if (previous === undefined) delete process.env.ADDP_E2E
    else process.env.ADDP_E2E = previous
  }
})
