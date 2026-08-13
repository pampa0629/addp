import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { executionDetailRoute } from '../src/utils/executionNavigation.js'
import { buildExecutionListRouteQuery, resolveExecutionListRouteState } from '../src/utils/executionListRouteState.js'
import { buildRuleApplicationListRouteQuery, resolveRuleApplicationListRouteState } from '../src/utils/ruleApplicationListRouteState.js'

const ruleApplicationSource = readFileSync(new URL('../src/views/RuleApplicationList.vue', import.meta.url), 'utf8')
const executionListSource = readFileSync(new URL('../src/views/ExecutionList.vue', import.meta.url), 'utf8')
const executionDetailSource = readFileSync(new URL('../src/views/ExecutionDetail.vue', import.meta.url), 'utf8')

test('rule application list route restores filters and pagination', () => {
  assert.deepEqual(resolveRuleApplicationListRouteState({
    engine_id: '2',
    schema_name: ' public ',
    table_name: ' customers ',
    page: '3',
    page_size: '50'
  }), {
    engineID: 2,
    schemaName: 'public',
    tableName: 'customers',
    page: 3,
    pageSize: 50,
    query: {
      engine_id: '2',
      schema_name: 'public',
      table_name: 'customers',
      page: '3',
      page_size: '50'
    },
    changed: true
  })
  assert.deepEqual(buildRuleApplicationListRouteQuery({
    engineID: null,
    schemaName: '',
    tableName: '',
    page: 1,
    pageSize: 20
  }), {})
})

test('rule application list route removes invalid and unknown values', () => {
  const state = resolveRuleApplicationListRouteState({
    engine_id: '-1',
    page: 'zero',
    page_size: '25',
    old: 'value'
  })
  assert.deepEqual(state.query, {})
  assert.equal(state.changed, true)
})

test('execution list route restores allowed status and pagination', () => {
  assert.deepEqual(resolveExecutionListRouteState({ status: 'failed', page: '2', page_size: '100' }), {
    status: 'failed',
    page: 2,
    pageSize: 100,
    query: { status: 'failed', page: '2', page_size: '100' },
    changed: false
  })
  assert.deepEqual(buildExecutionListRouteQuery({ status: '', page: 1, pageSize: 20 }), {})
})

test('execution list route removes unsupported status and invalid pagination', () => {
  const state = resolveExecutionListRouteState({ status: 'unknown', page: '-1', page_size: '25', old: 'value' })
  assert.deepEqual(state.query, {})
  assert.equal(state.changed, true)
})

test('execution detail route preserves canonical list context', () => {
  assert.deepEqual(executionDetailRoute(' execution-1 ', {
    status: 'success',
    page: '2',
    page_size: '50',
    unknown: 'value'
  }), {
    name: 'ExecutionDetail',
    params: { execution_id: 'execution-1' },
    query: { status: 'success', page: '2', page_size: '50' }
  })
  assert.equal(executionDetailRoute(''), null)
})

test('rule application list writes canonical filters and pagination into the route', () => {
  assert.match(ruleApplicationSource, /buildRuleApplicationListRouteQuery/)
  assert.match(ruleApplicationSource, /watch\(\(\) => route\.query, restoreListFromRoute/)
  assert.match(ruleApplicationSource, /Math\.ceil\(pagination\.value\.total \/ pagination\.value\.page_size\)/)
  assert.match(ruleApplicationSource, /history: 'replace'/)
})

test('execution list and detail preserve canonical list context', () => {
  assert.match(executionListSource, /executionDetailRoute\(executionId, route\.query\)/)
  assert.match(executionListSource, /watch\(\(\) => route\.query, restoreListFromRoute/)
  assert.match(executionListSource, /Math\.ceil\(pagination\.value\.total \/ pagination\.value\.page_size\)/)
  assert.match(executionDetailSource, /resolveExecutionListRouteState\(route\.query\)\.query/)
  assert.match(executionDetailSource, /watch\(\(\) => route\.fullPath/)
})
