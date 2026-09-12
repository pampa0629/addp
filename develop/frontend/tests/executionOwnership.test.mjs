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
  assert.match(detail, /scope="task"/)
  assert.match(detail, /retried\?\.execution_id/)
  assert.doesNotMatch(detail, /retried\?\.id/)
  assert.doesNotMatch(detail, /await handleRefresh\(\)/)
})
