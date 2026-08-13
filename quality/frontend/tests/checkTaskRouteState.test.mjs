import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildCheckTaskRouteQuery, resolveCheckTaskRouteState } from '../src/utils/checkTaskRouteState.js'

const viewSource = readFileSync(
  new URL('../src/views/CheckTaskList.vue', import.meta.url),
  'utf8'
)

test('check task route state restores canonical create and edit modes', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: '1' }), {
    mode: 'create',
    taskID: '',
    page: 1,
    pageSize: 20,
    query: { create: '1' },
    changed: false
  })
  assert.deepEqual(resolveCheckTaskRouteState({ task_id: '007' }), {
    mode: 'edit',
    taskID: '7',
    page: 1,
    pageSize: 20,
    query: { task_id: '7' },
    changed: true
  })
})

test('check task route state gives a valid task identity precedence over create', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: '1', task_id: '9' }), {
    mode: 'edit',
    taskID: '9',
    page: 1,
    pageSize: 20,
    query: { task_id: '9' },
    changed: true
  })
})

test('check task route state removes unknown and invalid query values', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ create: 'true', task_id: '-1', old: 'value' }), {
    mode: 'list',
    taskID: '',
    page: 1,
    pageSize: 20,
    query: {},
    changed: true
  })
})

test('check task route state restores pagination in list and dialog modes', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ page: '3', page_size: '50' }), {
    mode: 'list',
    taskID: '',
    page: 3,
    pageSize: 50,
    query: { page: '3', page_size: '50' },
    changed: false
  })
  assert.deepEqual(resolveCheckTaskRouteState({ create: '1', page: '2', page_size: '100' }).query, {
    create: '1',
    page: '2',
    page_size: '100'
  })
  assert.deepEqual(buildCheckTaskRouteQuery({ mode: 'list', taskID: '', page: 1, pageSize: 20 }), {})
})

test('check task route state removes invalid pagination and unknown values', () => {
  assert.deepEqual(resolveCheckTaskRouteState({ page: '-1', page_size: '25', old: 'value' }).query, {})
})

test('check task create and edit states are pushed into browser history', () => {
  assert.match(viewSource, /const requestCreateDialog = async \(\) => \{[\s\S]*?history: 'push'[\s\S]*?\n\}/)
  assert.match(viewSource, /const requestEditTask = async \(task\) => \{[\s\S]*?history: 'push'[\s\S]*?\n\}/)
})

test('check task page changes use canonical route state and recover out-of-range pages', () => {
  assert.match(viewSource, /const changePage = async \(page\) => \{[\s\S]*?await syncTaskRoute\(\)/)
  assert.match(viewSource, /Math\.ceil\(pagination\.value\.total \/ pagination\.value\.page_size\)/)
  assert.match(viewSource, /const pageChanged = pagination\.value\.page !== routeState\.page/)
})

test('check task engine display keeps historical engines separate from active selection', () => {
  assert.match(viewSource, /lifecycle_states: 'active,disabled'/)
  assert.match(viewSource, /const engineByID = computed/)
  assert.match(viewSource, /:disabled="engine\.lifecycle_state !== 'active'"/)
  assert.match(viewSource, /if \(!isActiveEngine\(form\.value\.engine_id\)\)/)
  assert.match(viewSource, /quality\.checkTask\.engineIdValue/)
})
