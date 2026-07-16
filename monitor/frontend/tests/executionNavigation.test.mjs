import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { executionDetailLocation } from '../src/utils/executionNavigation.js'

test('builds the execution detail route from the stable execution UUID', () => {
  assert.deepEqual(executionDetailLocation({
    id: 1582,
    execution_id: '26ec7f65-4a2f-4654-a375-5428dba6dd2c'
  }), {
    path: '/executions',
    query: {
      execution_id: '26ec7f65-4a2f-4654-a375-5428dba6dd2c'
    }
  })
})

test('does not fall back to the database row id', () => {
  assert.equal(executionDetailLocation({ id: 1582 }), null)
})

test('dashboard delegates recent execution details to the execution route', () => {
  const dashboardSource = readFileSync(
    new URL('../src/views/Dashboard.vue', import.meta.url),
    'utf8'
  )

  assert.match(dashboardSource, /const location = executionDetailLocation\(row\)/)
  assert.match(dashboardSource, /router\.push\(location\)/)
  assert.doesNotMatch(dashboardSource, /monitor\.dashboard\.view_execution/)
})
