import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

test('Develop keeps domain execution detail while Monitor owns its module-wide list', () => {
  const router = read('../src/router/index.js')
  const layout = read('../src/components/Layout.vue')
  const tasks = read('../src/views/TaskManagement.vue')
  const detail = read('../src/views/ExecutionDetail.vue')

  assert.doesNotMatch(router, /ExecutionMonitor/)
  assert.doesNotMatch(layout, /index=['"]\/executions['"]/)
  assert.match(router, /path:\s*['"]executions\/:execution_id['"]/)
  assert.match(tasks, /MonitorExecutionsButton module="develop"/)
  assert.match(detail, /MonitorExecutionsButton/)
  assert.match(detail, /scope="execution"/)
  assert.match(detail, /route\.query\.return_to/)
  assert.doesNotMatch(detail, /<el-descriptions/)
  assert.match(detail, /retried\?\.execution_id/)
  assert.doesNotMatch(detail, /retried\?\.id/)
  assert.doesNotMatch(detail, /await handleRefresh\(\)/)
})


test('execution result tab normalization preserves return context, including invalid input for rejection', async () => {
  const source = read('../src/utils/executionDetailRouteState.js').replace("'@common-ui/utils/recoverableRouteState'", JSON.stringify(new URL('../../../common-frontend/basic/src/utils/recoverableRouteState.js', import.meta.url).href))
  const { resolveExecutionDetailRouteState } = await import('data:text/javascript,' + encodeURIComponent(source))
  const return_to = '/monitor/executions?module=develop&page=3&execution_id=same-run'
  assert.deepEqual(resolveExecutionDetailRouteState({ return_to }), { tab: 'result', query: { return_to }, changed: false })
  assert.deepEqual(resolveExecutionDetailRouteState({ tab: 'logs', return_to }), { tab: 'logs', query: { tab: 'logs', return_to }, changed: false })
  assert.deepEqual(resolveExecutionDetailRouteState({ tab: 'unknown', return_to }), { tab: 'result', query: { return_to }, changed: true })
  for (const bad of ['', null, ['first', 'second'], 'https://other.invalid/']) {
    assert.deepEqual(resolveExecutionDetailRouteState({ return_to: bad }).query, { return_to: bad })
  }
})
