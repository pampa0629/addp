import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { buildDevelopQueryExecutionRoute, executionFailurePresentation, failureStages, failureReasons, operatorInputRules } from '../src/utils/executionFailure.mjs'

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
    diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'validation', reason: 'query_template_invalid' } }, key => key)
  assert.equal(presentation.stage, 'common.executionFailure.stages.validation')
  assert.equal(presentation.reason, 'common.executionFailure.reasons.query_template_invalid')
  assert.equal(presentation.code, 'develop.query.snapshot_invalid')
  assert.ok(!JSON.stringify(presentation).includes('secret'))
})

test('unknown or free text diagnostics show explicit missing evidence without runtime text', () => {
  for (const diagnostic of [undefined, { schema_version: 'v2', stage: 'validation', reason: 'query_template_invalid' },
    { schema_version: 'addp.execution-failure/v3', stage: 'secret', reason: 'query_failed' },
    { schema_version: 'addp.execution-failure/v3', stage: 'validation', reason: 'query_template_invalid', message: 'business-secret' }]) {
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
  const base = { schema_version: 'addp.execution-failure/v3', stage: 'preparation', reason: 'table_not_found' }
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

test('write diagnostics show only proven write column and independent target risk', () => {
  const translate = (key, params) => params ? `${key}:${JSON.stringify(params)}` : key
  const base = { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason: 'json_invalid' }
  const result = executionFailurePresentation({ diagnostic: { ...base, write: { column_index: 3, declared_type: 'json' }, target_change_possible: true }, message: 'private' }, translate)
  assert.match(result.write, /"column_index":3/)
  assert.equal(result.targetWarning, 'common.executionFailure.targetChangePossible')
  assert.doesNotMatch(JSON.stringify(result), /private/)
  for (const extra of [{ write: null }, { write: { column_index: 0, declared_type: 'json' } },
    { write: { column_index: 65536, declared_type: 'json' } }, { write: { column_index: 1.5, declared_type: 'json' } },
    { write: { column_index: 3, declared_type: 'bytes' } }, { write: { column_index: 3, declared_type: 'json', value: 'private' } },
    { target_change_possible: false }, { target_change_possible: 'true' }, { target_change_possible: null },
    { stage: 'validation', target_change_possible: true }, { schema_version: 'addp.execution-failure/v2' }]) {
    const invalid = executionFailurePresentation({ diagnostic: { ...base, ...extra } }, translate)
    assert.equal(invalid.write, '')
    assert.equal(invalid.targetWarning, '')
    assert.equal(invalid.stage, '')
  }
  const native = executionFailurePresentation({ diagnostic: { ...base, reason: 'table_write_failed', target_change_possible: true } }, translate)
  assert.equal(native.write, '')
  assert.ok(native.targetWarning)
  assert.equal(executionFailurePresentation({ diagnostic: base }, translate).targetWarning, '')
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
    const protectionText = messages.reasons.data_protection_required + messages.actions.data_protection_required
    assert.match(messages.reasons.data_protection_required, locale === 'zh-cn' ? /数据脱敏保护校验/ : /data masking protection checks/)
    assert.doesNotMatch(protectionText, /访问资格|读取权限|写入权限|access eligibility|read permission|write permission/i)
    assert.notEqual(messages.reasons.data_protection_required, messages.reasons.permission_denied)
    assert.notEqual(messages.reasons.data_protection_required, messages.reasons.authorization_invalid)
    assert.doesNotMatch(messages.reasons.data_protection_required, /查询|\bquery\b/i)
    for (const stage of ['preparation', 'execution']) assert.doesNotMatch(messages.stages[stage], /查询|\bquery\b/i)
    assert.doesNotMatch(messages.actions.data_protection_required, /SQL|字段别名|column aliases/i)
  }
  for (const stage of ['preparation', 'result']) {
    const presentation = executionFailurePresentation({ code: 'develop.query.protection_required',
      category: 'permission_denied', message: 'private-value SELECT phone',
      diagnostic: { schema_version: 'addp.execution-failure/v3', stage, reason: 'data_protection_required' }
    }, key => key)
    assert.equal(presentation.stage, `common.executionFailure.stages.${stage}`)
    assert.equal(presentation.reason, 'common.executionFailure.reasons.data_protection_required')
    assert.equal(presentation.action, 'common.executionFailure.actions.data_protection_required')
    assert.doesNotMatch(JSON.stringify(presentation), /private-value|SELECT/)
  }
  for (const reason of ['permission_denied', 'authorization_invalid']) {
    const presentation = executionFailurePresentation({ category: 'execution_failed',
      diagnostic: { schema_version: 'addp.execution-failure/v3', stage: 'authorization', reason }
    }, key => key)
    assert.equal(presentation.reason, `common.executionFailure.reasons.${reason}`)
    assert.equal(presentation.action, `common.executionFailure.actions.${reason}`)
  }
})


test('operator input contract matches the canonical runtime tuples and rejects unproven fields', () => {
  const source = read('../../../common-python/addp_common/workflow_runtime/errors.py')
  const canonical = [...source.matchAll(/\('([^']+)', '([^']+)'\): \('([^']*)', '([^']*)'\)/g)].map(match => match.slice(1))
  assert.equal(canonical.length, 11)
  const actual = Object.entries(operatorInputRules).flatMap(([op, rules]) => Object.entries(rules).map(([reason, rule]) => [op, reason, ...rule]))
  assert.deepEqual(actual.sort(), canonical.sort())
  for (const [operator_id, reason, parameter, field] of canonical) {
    const operator = { node_index: 2, operator_id, ...(parameter ? { parameter } : {}), ...(field ? { field } : {}) }
    const diagnostic = { schema_version: 'addp.execution-failure/v3', stage: 'execution', reason, operator }
    const result = executionFailurePresentation({ diagnostic, message: 'private' }, key => key)
    assert.equal(result.reason, `common.executionFailure.reasons.${reason}`)
    assert.ok(result.node)
    assert.equal(result.field, field)
    for (const bad of [
      { ...diagnostic, stage: 'validation' }, { ...diagnostic, operator: null },
      { ...diagnostic, operator: { ...operator, node_index: true } },
      { ...diagnostic, operator: { ...operator, node_index: 65536 } },
      { ...diagnostic, operator: { ...operator, node_id: 'private' } },
      { ...diagnostic, operator: { ...operator, parameter: null } },
      { ...diagnostic, operator: { ...operator, field: 'password' } },
      { ...diagnostic, sql: { position: { line: 1, column: 1 } } },
      { ...diagnostic, reason: 'execution_failed' }
    ]) {
      const rejected = executionFailurePresentation({ diagnostic: bad, message: 'private' }, key => key)
      assert.equal(rejected.operator, '')
      assert.equal(rejected.stage, '')
      assert.doesNotMatch(JSON.stringify(rejected), /private|password/)
    }
  }
})

test('controlled input messages distinguish ordinary and active geometry conflicts in both languages', () => {
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(read(`../src/i18n/${locale}.json`)).common.executionFailure
    const translate = (key, params = {}) => key.replace('common.executionFailure.', '').split('.').reduce((node, part) => node[part], messages)
      .replace(/\{(\w+)\}/g, (_, key) => params[key])
    for (const [operator_id, reason] of [['batch_buffer','operator_field_conflict'], ['distance_to','operator_geometry_field_conflict']]) {
      const presentation = executionFailurePresentation({ diagnostic: { schema_version:'addp.execution-failure/v3',stage:'execution',reason,
        operator:{node_index:2, operator_id,parameter:'input_gdf',field:'distance'} },message:'private' }, translate)
      assert.match(presentation.reason, /distance/)
      assert.doesNotMatch(presentation.reason, /distances/)
      assert.match(presentation.node, /2/)
      assert.match(presentation.action, locale === 'zh-cn' ? /重命名/ : /[Rr]ename/)
      assert.doesNotMatch(JSON.stringify(presentation), /private/)
    }
    assert.notEqual(messages.reasons.operator_field_conflict, messages.reasons.operator_geometry_field_conflict)
  }
})
