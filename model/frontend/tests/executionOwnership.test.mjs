import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const logicalTables = readFileSync(new URL('../src/views/LogicalTableList.vue', import.meta.url), 'utf8')
const groups = readFileSync(new URL('../src/views/MaterializationGroupList.vue', import.meta.url), 'utf8')

test('Model task definition workspaces expose the shared Monitor entry', () => {
  assert.match(logicalTables, /MonitorExecutionsButton module="model"/)
  assert.match(groups, /MonitorExecutionsButton[^>]+module="model"[^>]+task-type="materialization_group_publish"/)
})
