import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { buildPlanRouteQuery, resolvePlanRouteState } from '../src/utils/planRouteState.js'

const viewSource = readFileSync(
  new URL('../src/views/PlanList.vue', import.meta.url),
  'utf8'
)

test('plan route state restores canonical create and edit modes', () => {
  assert.deepEqual(resolvePlanRouteState({ create: '1' }), {
    mode: 'create',
    taskID: '',
    ownerDomainID: null,
    page: 1,
    pageSize: 20,
    query: { create: '1' },
    changed: false
  })
  assert.deepEqual(resolvePlanRouteState({ task_id: '007' }), {
    mode: 'edit',
    taskID: '7',
    ownerDomainID: null,
    page: 1,
    pageSize: 20,
    query: { task_id: '7' },
    changed: true
  })
})

test('plan route state gives a valid task identity precedence over create', () => {
  assert.deepEqual(resolvePlanRouteState({ create: '1', task_id: '9' }), {
    mode: 'edit',
    taskID: '9',
    ownerDomainID: null,
    page: 1,
    pageSize: 20,
    query: { task_id: '9' },
    changed: true
  })
})

test('plan route state removes unknown and invalid query values', () => {
  assert.deepEqual(resolvePlanRouteState({ create: 'true', task_id: '-1', old: 'value' }), {
    mode: 'list',
    taskID: '',
    ownerDomainID: null,
    page: 1,
    pageSize: 20,
    query: {},
    changed: true
  })
})

test('plan route state restores pagination in list and dialog modes', () => {
  assert.deepEqual(resolvePlanRouteState({ page: '3', page_size: '50' }), {
    mode: 'list',
    taskID: '',
    page: 3,
    ownerDomainID: null,
    pageSize: 50,
    query: { page: '3', page_size: '50' },
    changed: false
  })
  assert.deepEqual(resolvePlanRouteState({ create: '1', page: '2', page_size: '100' }).query, {
    create: '1',
    page: '2',
    page_size: '100'
  })
  assert.deepEqual(buildPlanRouteQuery({ mode: 'list', taskID: '', page: 1, pageSize: 20 }), {})
})

test('plan route state removes invalid pagination and unknown values', () => {
  assert.deepEqual(resolvePlanRouteState({ page: '-1', page_size: '25', old: 'value' }).query, {})
})
