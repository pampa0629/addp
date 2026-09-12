import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'

const component = fs.readFileSync(new URL('../src/components/MonitorExecutionsButton.vue', import.meta.url), 'utf8')
const publicEntry = fs.readFileSync(new URL('../src/index.js', import.meta.url), 'utf8')

test('MonitorExecutionsButton owns the standard module and task history navigation', () => {
  assert.match(component, /openMonitorExecutions/)
  assert.match(component, /module: props\.module/)
  assert.match(component, /task_type: props\.taskType/)
  assert.match(component, /source_task_id: props\.sourceTaskId/)
  assert.match(component, /common\.executionMonitor\.viewExecutions/)
  assert.match(component, /common\.executionMonitor\.viewTaskExecutions/)
  assert.match(publicEntry, /MonitorExecutionsButton/)
})
