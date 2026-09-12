import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

test('Quality keeps domain result detail while Monitor owns its module-wide list', () => {
  const router = read('../src/router/index.js')
  const layout = read('../src/components/Layout.vue')
  const checks = read('../src/views/CheckTaskList.vue')
  const gates = read('../src/views/MaterializationGateTaskList.vue')
  const detail = read('../src/views/ExecutionDetail.vue')

  assert.doesNotMatch(router, /name:\s*['"]ExecutionList['"]/)
  assert.doesNotMatch(layout, /index=['"]\/executions['"]/)
  assert.match(router, /path:\s*['"]executions\/:execution_id['"]/)
  assert.match(checks, /MonitorExecutionsButton[^>]+module="quality"[^>]+task-type="check"/)
  assert.match(gates, /MonitorExecutionsButton[^>]+module="quality"[^>]+task-type="materialization_gate"/)
  assert.match(detail, /MonitorExecutionsButton/)
})
