import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildRuntimeTableTarget,
  querySourceValid,
  withQuerySource
} from '../src/views/TaskWizard/runtimeTarget.mjs'

test('runtime target uses the single existing-table append contract', () => {
  assert.deepEqual(buildRuntimeTableTarget(), {
    binding: 'runtime',
    data_type: 'table',
    representation: 'native',
    policy: { apply_mode: 'append' }
  })
})

test('query source is valid only for bounded native tables with a complete query', () => {
  const valid = {
    enabled: true,
    boundary: 'bounded',
    dataType: 'table',
    representation: 'native',
    language: 'mql',
    statement: '[{"$project":{"id":1}}]',
    parametersValid: true
  }
  assert.equal(querySourceValid(valid), true)
  assert.equal(querySourceValid({ ...valid, boundary: 'continuous' }), false)
  assert.equal(querySourceValid({ ...valid, parametersValid: false }), false)
})

test('query source keeps typed parameters without introducing target identity', () => {
  const endpoint = withQuerySource(
    { locator: 'addp://engine/1/path/outdoor/entries?type=table', data_type: 'table', representation: 'native' },
    { enabled: true, language: 'MQL', statement: ' [] ', parameters: { status: 'active' } }
  )
  assert.deepEqual(endpoint.query, {
    language: 'mql',
    statement: '[]',
    parameters: { status: 'active' }
  })
  assert.equal('target' in endpoint, false)
})
