import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { resolveCheckTaskRouteState } from '../src/utils/checkTaskRouteState.js'

const viewSource = readFileSync(
  new URL('../src/views/CheckTaskList.vue', import.meta.url),
  'utf8'
)

test('check task route state restores canonical create and edit modes', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: '1' }), {
    mode: 'create',
    taskID: '',
    query: { create: '1' },
    changed: false
  })
  assert.deepEqual(resolveCheckTaskRouteState({ task_id: '007' }), {
    mode: 'edit',
    taskID: '7',
    query: { task_id: '7' },
    changed: true
  })
})

test('check task route state gives a valid task identity precedence over create', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: '1', task_id: '9' }), {
    mode: 'edit',
    taskID: '9',
    query: { task_id: '9' },
    changed: true
  })
})

test('check task route state removes unknown and invalid query values', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: 'true', task_id: '-1', old: 'value' }), {
    mode: 'list',
    taskID: '',
    query: {},
    changed: true
  })
})

test('check task create and edit states are pushed into browser history', () => {
  assert.match(viewSource, /const requestCreateDialog = async \(\) => \{[\s\S]*?history: 'push'[\s\S]*?\n\}/)
  assert.match(viewSource, /const requestEditTask = async \(task\) => \{[\s\S]*?history: 'push'[\s\S]*?\n\}/)
})
