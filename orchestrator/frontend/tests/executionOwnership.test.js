import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const routerSource = readFileSync(resolve('src/router/index.js'), 'utf8')
const layoutSource = readFileSync(resolve('src/components/Layout.vue'), 'utf8')
const orchestrationListSource = readFileSync(resolve('src/views/OrchestrationList.vue'), 'utf8')
const executionListSource = readFileSync(resolve('src/views/ExecutionList.vue'), 'utf8')

test('Monitor owns the Orchestrator-wide execution list', () => {
  const moduleWideExecutionRoute = /path:\s*['"]executions['"]/
  assert.doesNotMatch(routerSource, moduleWideExecutionRoute)
  assert.equal(existsSync(resolve('src/views/ExecutionRecords.vue')), false)
  assert.doesNotMatch(layoutSource, /index="\/executions"/)
  assert.match(orchestrationListSource, /MonitorExecutionsButton module="orchestrator" task-type="orchestration"/)
})

test('the per-orchestration history retains domain details and links to filtered Monitor', () => {
  assert.match(routerSource, /path:\s*'orchestrations\/:id\/executions'/)
  assert.match(executionListSource, /orchestrationAPI\.listExecutions/)
  assert.match(executionListSource, /getStepResults/)
  assert.match(executionListSource, /openMonitorExecution/)
  assert.match(executionListSource, /MonitorExecutionsButton/)
  assert.match(executionListSource, /:source-task-id="route\.params\.id"/)
})
