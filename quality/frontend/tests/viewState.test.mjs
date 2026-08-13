import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const readView = (name) => readFileSync(
  new URL(`../src/views/${name}.vue`, import.meta.url),
  'utf8'
)

const ruleApplicationSource = readView('RuleApplicationList')
const checkTaskSource = readView('CheckTaskList')
const executionListSource = readView('ExecutionList')
const executionDetailSource = readView('ExecutionDetail')
const issueListSource = readView('IssueList')
const issueDetailSource = readView('IssueDetail')
const executionFailureSource = readFileSync(
  new URL('../src/utils/executionFailure.js', import.meta.url),
  'utf8'
)

test('rule application list renders the server-projected element summary', () => {
  assert.match(ruleApplicationSource, /row\.element\.name/)
  assert.match(ruleApplicationSource, /row\.element\.code/)
  assert.match(ruleApplicationSource, /quality\.ruleApplication\.elementIdValue/)
  assert.doesNotMatch(ruleApplicationSource, /elementCache/)
  assert.doesNotMatch(ruleApplicationSource, /elementName\(/)
})

test('rule application engine display keeps historical engines separate from active selection', () => {
  assert.match(ruleApplicationSource, /lifecycle_states: 'active,disabled'/)
  assert.match(ruleApplicationSource, /const activeEngines = computed/)
  assert.match(ruleApplicationSource, /v-for="eng in activeEngines"/)
  assert.match(ruleApplicationSource, /if \(!isActiveEngine\(form\.value\.engine_id\)\)/)
  assert.match(ruleApplicationSource, /quality\.ruleApplication\.engineIdValue/)
  assert.doesNotMatch(ruleApplicationSource, /return eng \? eng\.name : id/)
})

test('rule application creation uses the System catalog for schema and table selection', () => {
  assert.match(ruleApplicationSource, /systemCatalogAPI\.listChildren/)
  assert.match(ruleApplicationSource, /@change="onEngineChange"/)
  assert.match(ruleApplicationSource, /@change="onSchemaChange"/)
  assert.match(ruleApplicationSource, /@change="onTableChange"/)
  assert.match(ruleApplicationSource, /v-for="schema in schemaOptions"/)
  assert.match(ruleApplicationSource, /v-for="table in tableOptions"/)
  assert.match(ruleApplicationSource, /v-for="column in columnOptions"/)
  assert.doesNotMatch(ruleApplicationSource, /<el-input v-model="form\.schema_name"/)
  assert.doesNotMatch(ruleApplicationSource, /<el-input v-model="form\.table_name"/)
  assert.doesNotMatch(ruleApplicationSource, /<el-input v-model="form\.column_name"/)
})

test('rule application creation previews the enabled rule snapshot', () => {
  assert.match(ruleApplicationSource, /selectedElement\.value\?\.quality_rules/)
  assert.match(ruleApplicationSource, /document\.rules\.filter\(rule => rule\?\.enabled === true\)/)
  assert.match(ruleApplicationSource, /<el-table v-else :data="enabledRules"/)
  assert.match(ruleApplicationSource, /if \(!selectedElement\.value \|\| enabledRules\.value\.length === 0\)/)
  assert.match(ruleApplicationSource, /ruleApplicationAPI\.listElementCandidates/)
})

test('check task create and edit use the System catalog for schema and table selection', () => {
  assert.match(checkTaskSource, /systemCatalogAPI\.listChildren/)
  assert.match(checkTaskSource, /@change="onEngineChange"/)
  assert.match(checkTaskSource, /@change="onSchemaChange"/)
  assert.match(checkTaskSource, /v-for="schema in schemaOptions"/)
  assert.match(checkTaskSource, /v-for="table in tableOptions"/)
  assert.match(checkTaskSource, /catalogTargetAvailable\.value/)
  assert.doesNotMatch(checkTaskSource, /<el-input v-model="form\.schema_name"/)
  assert.doesNotMatch(checkTaskSource, /<el-input v-model="form\.table_name"/)
})

test('check task list projects the latest execution and polls only while active', () => {
  assert.match(checkTaskSource, /row\.last_execution_id/)
  assert.match(checkTaskSource, /executionDetailRoute\(executionID\)/)
  assert.match(checkTaskSource, /tasks\.value\.some\(isTaskActive\)/)
  assert.match(checkTaskSource, /window\.setTimeout\(fetchTasks, 2000\)/)
  assert.match(checkTaskSource, /:disabled="isTaskActive\(row\).*requestEditTask/)
  assert.match(checkTaskSource, /onBeforeUnmount/)
})

test('list failures clear stale rows and expose persistent errors', () => {
  for (const source of [ruleApplicationSource, executionListSource, issueListSource]) {
    assert.match(source, /loadError\.value = e\.response\?\.data\?\.error/)
    assert.match(source, /<el-alert v-if="loadError"/)
    assert.match(source, /\.value = \[\]/)
    assert.match(source, /pagination\.value\.total = 0/)
  }
  assert.match(checkTaskSource, /tasks\.value = \[\]/)
  assert.match(checkTaskSource, /<el-alert v-if="loadError"/)
})

test('write actions prevent duplicate requests while pending', () => {
  assert.match(ruleApplicationSource, /if \(submitting\.value\) return/)
  assert.match(ruleApplicationSource, /if \(updatingIds\.value\.has\(row\.id\)\) return/)
  assert.match(ruleApplicationSource, /if \(deletingIds\.value\.has\(id\)\) return/)
  assert.match(checkTaskSource, /if \(saving\.value\) return/)
  assert.match(checkTaskSource, /if \(runningTaskIds\.value\.has\(id\)\) return/)
  assert.match(checkTaskSource, /if \(deletingTaskIds\.value\.has\(id\)\) return/)
  assert.match(issueListSource, /if \(updatingIssueIds\.value\.has\(id\)\) return/)
  assert.match(issueDetailSource, /if \(updating\.value\) return/)
})

test('rule application enabled switch persists explicit state and rolls back failures', () => {
  assert.match(ruleApplicationSource, /ruleApplicationAPI\.update\(row\.id, \{ enabled \}\)/)
  assert.match(ruleApplicationSource, /row\.enabled = updated\.enabled/)
  assert.match(ruleApplicationSource, /row\.enabled = !enabled/)
  assert.match(ruleApplicationSource, /:loading="updatingIds\.has\(row\.id\)"/)
})

test('list requests ignore responses superseded by newer filters', () => {
  for (const source of [ruleApplicationSource, checkTaskSource, executionListSource, issueListSource]) {
    assert.match(source, /const requestSequence = \+\+listRequestSequence/)
    assert.match(source, /if \(requestSequence !== listRequestSequence\) return/)
  }
  assert.match(ruleApplicationSource, /const searchSequence = \+\+elementSearchSequence/)
})

test('execution detail shows a stable failure state and stops obsolete polling', () => {
  assert.match(executionDetailSource, /<el-result[\s\S]*?v-if="loadError"/)
  assert.match(executionDetailSource, /watch\(\(\) => route\.fullPath/)
  assert.match(executionDetailSource, /if \(requestSequence !== loadSequence\) return/)
  assert.match(executionDetailSource, /onBeforeUnmount\(\(\) => \{[\s\S]*?window\.clearTimeout\(pollTimer\)/)
  assert.match(executionDetailSource, /failureReason/)
  assert.match(executionDetailSource, /executionFailureLabel\(execution\.value, t\)/)
})

test('execution list and detail share localized stable failure reasons', () => {
  assert.match(executionListSource, /executionFailureLabel\(row, t\)/)
  assert.match(executionDetailSource, /executionFailureLabel\(execution\.value, t\)/)
  assert.match(executionFailureSource, /execution\.error_details\?\.code/)
  assert.match(executionFailureSource, /quality\.execution\.failureUnknown/)
  assert.doesNotMatch(executionListSource, /error_details\?\.message/)
  assert.doesNotMatch(executionDetailSource, /error_details\?\.message/)
})
