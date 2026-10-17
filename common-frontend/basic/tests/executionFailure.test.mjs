import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { buildDevelopQueryExecutionRoute, executionFailurePresentation, failureStages, failureReasons } from '../src/utils/executionFailure.mjs'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')

test('failure diagnostics use only closed shared vocabulary in both languages', () => {
  const backend = read('../../../common/execution/failure_diagnostic.go')
  const backendStages = [...backend.split('var failureStages =')[1].split('var failureReasons =')[0].matchAll(/"([a-z_]+)":\s*true/g)].map(item => item[1])
  const backendReasons = [...backend.split('var failureReasons =')[1].split('func NewFailureDiagnostic')[0].matchAll(/"([a-z_]+)":\s*true/g)].map(item => item[1])
  assert.deepEqual([...failureStages].sort(), backendStages.sort())
  assert.deepEqual([...failureReasons].sort(), backendReasons.sort())
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    for (const stage of failureStages) assert.ok(messages.stages[stage])
    for (const reason of failureReasons) {
      assert.ok(messages.reasons[reason])
      assert.ok(messages.actions[reason])
    }
  }
  const presentation = executionFailurePresentation({ code: 'develop.query.snapshot_invalid', message: 'secret runtime body',
    diagnostic: { schema_version: 'addp.execution-failure/v2', stage: 'validation', reason: 'query_template_invalid' } }, key => key)
  assert.equal(presentation.stage, 'common.executionFailure.stages.validation')
  assert.equal(presentation.reason, 'common.executionFailure.reasons.query_template_invalid')
  assert.equal(presentation.code, 'develop.query.snapshot_invalid')
  assert.ok(!JSON.stringify(presentation).includes('secret'))
})

test('unknown or free text diagnostics show explicit missing evidence without runtime text', () => {
  for (const diagnostic of [undefined, { schema_version: 'v2', stage: 'validation', reason: 'query_template_invalid' },
    { schema_version: 'addp.execution-failure/v2', stage: 'secret', reason: 'query_failed' },
    { schema_version: 'addp.execution-failure/v2', stage: 'validation', reason: 'query_template_invalid', message: 'business-secret' }]) {
    const result = executionFailurePresentation({ diagnostic, category: 'unknown', code: 'secret-value', message: 'private' }, key => key)
    assert.equal(result.stage, '')
    assert.equal(result.reason, 'common.executionFailure.execution_failed')
    assert.equal(result.action, 'common.executionFailure.unrecordedAction')
    assert.equal(result.code, '')
  }
})

test('existing orchestration failure categories retain their specific explanation', () => {
  for (const category of ['submission_uncertain', 'coordinator_lost', 'child_failed', 'owner_unavailable']) {
    const result = executionFailurePresentation({ category, code: 'orchestrator.' + category }, key => key)
    assert.equal(result.reason, `common.executionFailure.${category}`)
  }
})

test('SQL failure facts accept only bounded identifiers and coordinates under the single current contract', () => {
  const translate = (key, params) => params ? `${key}:${JSON.stringify(params)}` : key
  const base = { schema_version: 'addp.execution-failure/v2', stage: 'preparation', reason: 'table_not_found' }
  const result = executionFailurePresentation({ diagnostic: { ...base, sql: { object: { kind: 'table', name: 'public.farmla1nd' }, position: { line: 2, column: 15 } } } }, translate)
  assert.match(result.object, /public.farmla1nd/)
  assert.match(result.position, /"line":2/)
  for (const sql of [{ object: { kind: 'message', name: 'private' } }, { object: { kind: 'table', name: 'a\nprivate' } },
    { object: { kind: 'table', name: 'a', message: 'private' } }, { position: { line: 1, column: 0 } },
    { position: { line: 1, column: 1, detail: 'private' } }, { object: { kind: 'table', name: '\ud800' } }]) {
    const invalid = executionFailurePresentation({ diagnostic: { ...base, sql } }, translate)
    assert.equal(invalid.object, '')
    assert.equal(invalid.position, '')
  }
  assert.equal(executionFailurePresentation({ diagnostic: { ...base, schema_version: 'addp.execution-failure/v1' } }, translate).stage, '')
})

test('query execution navigation works without a task definition and does not guess other owners', () => {
  assert.equal(buildDevelopQueryExecutionRoute({ module: 'develop', task_type: 'query', execution_id: 'execution/id' }), '/develop/executions/execution%2Fid')
  for (const input of [{ module: 'develop', task_type: 'workflow', execution_id: 'id' }, { module: 'meta', task_type: 'query', execution_id: 'id' }, {}]) {
    assert.equal(buildDevelopQueryExecutionRoute(input), '')
  }
})

test('shared ExecutionFailure is the sole failure panel for Workbench, Owner and Monitor', () => {
  for (const path of ['../../../develop/frontend/src/components/QueryResult.vue', '../../../develop/frontend/src/views/ExecutionDetail.vue', '../../../monitor/frontend/src/views/ExecutionList.vue']) {
    const source = read(path)
    assert.equal((source.match(/<ExecutionFailure\b/g) || []).length, 1)
    assert.doesNotMatch(source, /executionErrorSummary|executionErrorMessage|error-traceback|name="error"/)
  }
})

test('protection refusals remain distinct from data permissions without exposing runtime text', () => {
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    assert.doesNotMatch(messages.reasons.data_protection_required, /查询|\bquery\b/i)
    assert.doesNotMatch(messages.actions.data_protection_required, /SQL|字段别名|column aliases/i)
  }
  for (const stage of ['preparation', 'result']) {
    const presentation = executionFailurePresentation({ code: 'develop.query.protection_required',
      category: 'permission_denied', message: 'private-value SELECT phone',
      diagnostic: { schema_version: 'addp.execution-failure/v2', stage, reason: 'data_protection_required' }
    }, key => key)
    assert.equal(presentation.stage, `common.executionFailure.stages.${stage}`)
    assert.equal(presentation.reason, 'common.executionFailure.reasons.data_protection_required')
    assert.equal(presentation.action, 'common.executionFailure.actions.data_protection_required')
    assert.doesNotMatch(JSON.stringify(presentation), /private-value|SELECT/)
  }
})
