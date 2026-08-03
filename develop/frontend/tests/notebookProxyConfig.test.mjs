import assert from 'node:assert/strict'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { loadConfigFromFile } from 'vite'

const testDirectory = path.dirname(fileURLToPath(import.meta.url))

test('develop dev proxy forwards notebook WebSocket connections', async () => {
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'development' },
    path.resolve(testDirectory, '../vite.config.js')
  )

  assert.equal(loaded?.config.server.proxy['/api'].ws, true)
})
