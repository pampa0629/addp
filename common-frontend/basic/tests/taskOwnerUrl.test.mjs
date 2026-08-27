import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveConsoleOrigin } from '../src/utils/consoleOrigin.js'

test('resolves every registered module frontend development port to Console origin', () => {
  for (const port of ['5173', '5188', '5189', '5190']) {
    assert.equal(resolveConsoleOrigin({
      origin: `http://localhost:${port}`,
      protocol: 'http:',
      hostname: 'localhost',
      port
    }), 'http://localhost:5170')
  }
})
