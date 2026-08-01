import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveMetadataScanRouteState } from '../src/utils/routeState.js'

const engines = [{ id: 2, name: 'PostgreSQL' }, { id: 3, name: 'MySQL' }]
const tasks = [{ id: 9, engine_id: 3 }]

test('scan homepage canonicalizes to the first engine', () => {
  assert.deepEqual(resolveMetadataScanRouteState(engines, tasks, {}), {
    kind: 'redirect',
    query: { engine_id: '2' }
  })
})

test('valid engine selection is stable and removes unrelated query state', () => {
  assert.deepEqual(resolveMetadataScanRouteState(engines, tasks, { engine_id: '2' }), {
    kind: 'ready',
    engine: engines[0],
    task: null,
    taskId: '',
    query: { engine_id: '2' },
    changed: false
  })
  assert.equal(resolveMetadataScanRouteState(engines, tasks, {
    engine_id: '2',
    legacy: 'value'
  }).changed, true)
})

test('task ownership overrides a conflicting engine query', () => {
  assert.deepEqual(resolveMetadataScanRouteState(engines, tasks, {
    engine_id: '2',
    task_id: '9'
  }), {
    kind: 'ready',
    engine: engines[1],
    task: tasks[0],
    taskId: '9',
    query: { engine_id: '3', task_id: '9' },
    changed: true
  })
})

test('invalid task and engine identities remain explicit errors', () => {
  assert.deepEqual(resolveMetadataScanRouteState(engines, tasks, { task_id: '999' }), {
    kind: 'task-unavailable',
    taskId: '999',
    engine: null
  })
  assert.deepEqual(resolveMetadataScanRouteState(engines, tasks, { engine_id: '999' }), {
    kind: 'engine-unavailable',
    engineId: '999',
    task: null,
    taskId: ''
  })
})

test('a task whose owner engine is unavailable reports the engine error', () => {
  const missingOwnerTask = { id: 10, engine_id: 99 }
  assert.deepEqual(resolveMetadataScanRouteState(engines, [missingOwnerTask], { task_id: '10' }), {
    kind: 'engine-unavailable',
    engineId: '99',
    task: missingOwnerTask,
    taskId: '10'
  })
})
