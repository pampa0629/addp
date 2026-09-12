import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'

const routerSource = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const taskListSource = readFileSync(new URL('../src/views/TaskList.vue', import.meta.url), 'utf8')
const taskDetailSource = readFileSync(new URL('../src/views/TaskDetail.vue', import.meta.url), 'utf8')
const executionDetailSource = readFileSync(new URL('../src/views/ExecutionDetail.vue', import.meta.url), 'utf8')

test('Monitor owns the Transfer-wide execution list', () => {
  assert.doesNotMatch(routerSource, /path:\s*['"]executions['"]/)
  assert.match(routerSource, /path:\s*['"]executions\/:execution_id['"]/)
  assert.equal(existsSync(new URL('../src/views/ExecutionList.vue', import.meta.url)), false)
  assert.equal(existsSync(new URL('../src/views/Dashboard.vue', import.meta.url)), false)

  assert.match(taskListSource, /MonitorExecutionsButton module="transfer" task-type="sync"/)
})

test('Transfer task details retain domain history and link to the filtered Monitor view', () => {
  assert.match(taskDetailSource, /data-testid="task-executions"/)
  assert.match(taskDetailSource, /MonitorExecutionsButton/)
  assert.match(taskDetailSource, /module="transfer"/)
  assert.match(taskDetailSource, /task-type="sync"/)
  assert.match(taskDetailSource, /:source-task-id="route\.params\.id"/)
  assert.match(taskDetailSource, /navigateTransferRoute\(router, `\/executions\/\$\{executionId\}`\)/)
  assert.match(executionDetailSource, /MonitorExecutionsButton/)
  assert.match(executionDetailSource, /:source-task-id="execution\.task_id"/)
  assert.doesNotMatch(executionDetailSource, /execution\.source_task_id/)
})
