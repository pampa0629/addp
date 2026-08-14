import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { issueDetailRoute, issueExecutionRoute } from '../src/utils/issueNavigation.js'
import { buildIssueListRouteQuery, resolveIssueListRouteState } from '../src/utils/issueListRouteState.js'

const listSource = readFileSync(new URL('../src/views/IssueList.vue', import.meta.url), 'utf8')
const detailSource = readFileSync(new URL('../src/views/IssueDetail.vue', import.meta.url), 'utf8')
const routerSource = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')

test('issue detail links use the canonical issue route', () => {
  assert.deepEqual(issueDetailRoute('007'), {
    name: 'IssueDetail',
    params: { id: '7' },
    query: {}
  })
  assert.equal(issueDetailRoute(''), null)
  assert.equal(issueDetailRoute('-1'), null)
  assert.equal(issueDetailRoute('unsafe'), null)
  assert.match(routerSource, /path: 'issues\/:id',[\s\S]*?name: 'IssueDetail'/)
  assert.match(listSource, /openIssue\(row\.id\)/)
})

test('issue list route state restores filters and pagination', () => {
  assert.deepEqual(resolveIssueListRouteState({ status: 'ignored', engine_id: '2', page: '3', page_size: '50' }), {
    status: 'ignored',
    engineID: 2,
    page: 3,
    pageSize: 50,
    query: { status: 'ignored', engine_id: '2', page: '3', page_size: '50' },
    changed: false
  })
  assert.deepEqual(buildIssueListRouteQuery({ status: '', engineID: null, page: 1, pageSize: 20 }), {})
})

test('issue list route state removes unsupported values and defaults', () => {
  assert.deepEqual(resolveIssueListRouteState({ status: 'closed', engine_id: '-1', page: 'zero', page_size: '25', old: 'value' }).query, {})
  assert.equal(resolveIssueListRouteState({ status: 'closed' }).changed, true)
})

test('issue detail preserves canonical list context', () => {
  assert.deepEqual(issueDetailRoute(9, { status: 'open', engine_id: '2', page: '2', unknown: 'value' }), {
    name: 'IssueDetail',
    params: { id: '9' },
    query: { status: 'open', engine_id: '2', page: '2' }
  })
  assert.match(detailSource, /resolveIssueListRouteState\(route\.query\)\.query/)
})

test('issue execution links use the canonical execution detail route', () => {
  assert.deepEqual(issueExecutionRoute(' execution-1 '), {
    name: 'ExecutionDetail',
    params: { execution_id: 'execution-1' }
  })
})

test('issue execution links ignore absent execution identities', () => {
  assert.equal(issueExecutionRoute(''), null)
  assert.equal(issueExecutionRoute('   '), null)
  assert.equal(issueExecutionRoute(null), null)
})

test('issue list and detail expose first and last observed executions', () => {
  assert.match(listSource, /openExecution\(row\.execution_id\)/)
  assert.match(listSource, /openExecution\(row\.last_execution_id\)/)
  assert.match(listSource, /<el-button[\s\S]*?class="execution-link"[\s\S]*?link/)
  assert.match(detailSource, /openExecution\(issue\.execution_id\)/)
  assert.match(detailSource, /openExecution\(issue\.last_execution_id\)/)
})

test('issue detail reloads after manual status changes and shows audit facts', () => {
  assert.match(detailSource, /await issueAPI\.updateStatus\(issue\.value\.id, status, note\)[\s\S]*?await loadIssue\(\)/)
  assert.match(detailSource, /issue\.value\?\.resolved_by != null/)
  assert.match(detailSource, /quality\.issue\.manualResolution/)
  assert.match(detailSource, /quality\.issue\.automaticResolution/)
  assert.match(detailSource, /quality\.issue\.resolutionNote/)
})

test('issue detail exposes the stable rule identity', () => {
  assert.match(detailSource, /issue\.rule_key/)
  assert.match(detailSource, /quality\.issue\.ruleKey/)
})

test('issue list writes filters and pagination into the canonical route', () => {
  assert.match(listSource, /buildIssueListRouteQuery/)
  assert.match(listSource, /watch\(\(\) => route\.query, restoreListFromRoute/)
  assert.match(listSource, /Math\.ceil\(pagination\.value\.total \/ pagination\.value\.page_size\)/)
  assert.match(listSource, /await syncRoute\(\)[\s\S]*?await fetchList\(\)/)
})
