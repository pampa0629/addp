import assert from 'node:assert/strict'
import test from 'node:test'

import { engineNameForID } from '../src/utils/engineDisplay.mjs'

test('engine display resolves the System engine name without exposing its id', () => {
  const engines = [
    { id: 2, name: 'Business PostgreSQL' },
    { id: 11, name: 'Business MongoDB' }
  ]

  assert.equal(engineNameForID(engines, 11), 'Business MongoDB')
  assert.equal(engineNameForID(engines, 2), 'Business PostgreSQL')
  assert.equal(engineNameForID(engines, 99), '-')
})
