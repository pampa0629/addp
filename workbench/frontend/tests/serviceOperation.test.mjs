import assert from 'node:assert/strict'
import test from 'node:test'
import { assertQueryOperation } from '../src/utils/serviceOperation.mjs'

test('accepts only the same-origin Service query operation contract', () => {
  const operation = {
    key: 'query', method: 'POST', path: '/api/query/orders/query',
    input_kind: 'structured_query', output_kind: 'tabular'
  }
  assert.equal(assertQueryOperation(operation), operation)
  assert.throws(() => assertQueryOperation({ ...operation, path: 'https://example.invalid/collect' }))
  assert.throws(() => assertQueryOperation({ ...operation, path: '//example.invalid/collect' }))
  assert.throws(() => assertQueryOperation({ ...operation, method: 'GET' }))
})
