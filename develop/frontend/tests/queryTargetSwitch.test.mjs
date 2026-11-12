import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { readFile } from 'node:fs/promises'
import { createLatestRequestScope } from '../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'

const queryEditor = await readFile(new URL('../src/views/QueryEditor.vue', import.meta.url), 'utf8')

const coordinator = createLatestRequestCoordinator()
const duckdbRequest = coordinator.begin('mode:duckdb')
const dorisRequest = coordinator.begin('engine:17')

assert.equal(coordinator.isCurrent(duckdbRequest, 'engine:17'), false)
assert.equal(coordinator.isCurrent(dorisRequest, 'engine:17'), true)

coordinator.invalidate()
assert.equal(coordinator.isCurrent(dorisRequest, 'engine:17'), false)

const switchTargetSource = queryEditor.slice(
  queryEditor.indexOf('async function requestQueryTargetChange'),
  queryEditor.indexOf('const executeQuery = async')
)
const executeQuerySource = queryEditor.slice(
  queryEditor.indexOf('const executeQuery = async'),
  queryEditor.indexOf('const formatQuery =')
)
const queryParameterPanelStart = queryEditor.indexOf('class="query-parameter-panel"')
const queryParameterPanelSource = queryEditor.slice(
	  queryParameterPanelStart,
	  queryEditor.indexOf('</component>', queryParameterPanelStart)
)

assert.match(queryEditor, /:model-value="selectedQueryTarget"/)
assert.match(queryEditor, /@change="requestQueryTargetChange"/)
assert.match(queryEditor, /:disabled="executing \|\| loadingSampleQuery \|\| switchingQueryTarget \|\| savingForEngineSwitch"/)
assert.match(switchTargetSource, /!isDirty\.value/)
assert.match(switchTargetSource, /await applyQueryTargetSwitch\(targetValue\)/)
assert.match(queryEditor, /async function applyQueryTargetSwitch/)
assert.match(queryEditor, /queryContent\.value = ''/)
assert.match(queryEditor, /queryParameters\.value = \[\]/)
assert.doesNotMatch(switchTargetSource, /await loadSampleQuery\(\{ replace: true \}\)/)
assert.match(queryEditor, /bypassUnsavedRouteConfirm\.value = true/)
assert.match(queryEditor, /isDirty: \(\) => !bypassUnsavedRouteConfirm\.value && isDirty\.value/)
assert.match(queryEditor, /develop\.query\.clearAndSwitch/)
assert.match(queryEditor, /develop\.query\.saveAndClear/)
assert.match(queryEditor, /mode="any"/)
assert.match(queryEditor, /:engine-id="catalogTreeEngineValue"/)
assert.match(queryEditor, /:engine-multiple="catalogTreeEngineMultiple"/)
assert.match(queryEditor, /listResourceTreeEngines\('\/api\/v1\/meta'\)/)
assert.match(queryEditor, /supportedTypes\.has\(String\(source\.engine_type/)
assert.match(queryEditor, /:selectable-filter="isQueryCatalogSelectionAllowed"/)
assert.match(queryEditor, /isQueryCatalogSelectionAllowed/)
assert.match(queryEditor, /getResourceFields\('\/api\/v1\/meta'/)
assert.match(queryEditor, /const itemId = selection\?\.identity\?\.item_id \|\| parsed\.itemId/)
assert.match(queryEditor, /if \(!itemId\) return/)
assert.match(queryEditor, /kind: 'field'/)
assert.match(queryEditor, /insertText: fieldInsertionText\(name\)/)
assert.match(queryEditor, /:completions="completionSuggestions"/)
assert.match(queryEditor, /@node-click="rememberCatalogNode"/)
assert.match(queryEditor, /@node-expand="rememberCatalogNode"/)
assert.match(queryEditor, /getResourceTreeNode\('\/api\/v1\/meta'/)
assert.match(queryEditor, /engine_id: parsed\.engineId/)
assert.match(queryEditor, /getResourceFacts\('\/api\/v1\/manager', locator\)/)
assert.match(queryEditor, /renderQueryReference\(/)
assert.match(queryEditor, /const queryExecutionTargetLocator = computed\(/)
assert.match(queryEditor, /federatedQuery\.value \|\| hasRelationParameters\.value/)
assert.doesNotMatch(queryEditor, /target_locator: hasRelationParameters\.value/)
assert.doesNotMatch(queryEditor, /federatedIdentifier/)
assert.match(queryEditor, /preflightQuery/)
assert.match(queryEditor, /develop\.query\.queryDiagnostics/)
assert.match(queryEditor, /triggerCompletion/)
assert.match(queryEditor, /showCompletions/)
assert.match(queryEditor, /refreshQueryDiagnostics/)
assert.match(queryEditor, /generateQueryTemplate/)
assert.match(queryEditor, /develop\.query\.queryParameters/)
assert.match(queryParameterPanelSource, /v-for="\(parameter, index\) in queryParameters"/)
assert.match(queryParameterPanelSource, /parameterTypes\.\$\{type\}/)
assert.match(queryParameterPanelSource, /parameter\.type !== 'relation'/)
assert.match(queryEditor, /parameter\?\.type === 'relation'[\s\S]*String\(parameter\?\.name/)
assert.doesNotMatch(queryEditor, /input\.[A-Za-z_$]/)
assert.doesNotMatch(queryEditor, /\|\| hasRelationParameters \|\| switchingQueryTarget/)
assert.doesNotMatch(queryEditor, /relation_inputs|addp_input|input_locators/)
const monacoEditorSource = await readFile(new URL('../src/components/MonacoEditor.vue', import.meta.url), 'utf8')
assert.match(monacoEditorSource, /editor\.getContribution\('editor\.contrib\.suggestController'\)/)
assert.match(monacoEditorSource, /contrib\/suggest\/browser\/suggestController\.js/)
assert.doesNotMatch(monacoEditorSource, /editor\.trigger\('keyboard', 'editor\.action\.triggerSuggest'/)
assert.match(monacoEditorSource, /contextInsertText/)
assert.match(monacoEditorSource, /sortText: item\.kind === 'field'/)
assert.match(monacoEditorSource, /item\.kind === 'field'/)
assert.match(monacoEditorSource, /CompletionItemKind\.Field/)
assert.match(monacoEditorSource, /monaco\.languages\.register\(\{ id: 'mql' \}\)/)
assert.match(monacoEditorSource, /window\.setTimeout\(triggerSuggest, 0\)/)
assert.doesNotMatch(queryEditor, /:disabled="[^"]*catalogSelection\?\.identity\?\.locator[^"]*"/)
assert.match(queryEditor, /ElMessage\.warning\(t\('develop\.query\.selectResourceForQueryTemplate'\)\)/)
assert.match(queryEditor, /@node-dblclick="insertCatalogItemAtCursor"/)
assert.match(queryEditor, /insertCatalogItemAtCursor/)
assert.match(queryEditor, /editorRef\.value\?\.insertText\(/)
assert.doesNotMatch(queryEditor, /insertCatalogPath/)
assert.doesNotMatch(queryEditor, /insertResourcePath/)
assert.match(executeQuerySource, /if \(loadingSampleQuery\.value \|\| executing\.value\) return/)
assert.match(queryEditor, /sampleRequests\.isCurrent\(request, `\$\{selectedQueryTarget\.value\}:\$\{locator\}`\)/)
assert.match(queryEditor, /createExecution\(\{/)
assert.match(queryEditor, /query_parameters: queryParameters\.value\.map\(queryParameterPayload\)/)
assert.match(queryEditor, /parameters,/)
assert.match(queryEditor, /<ExecutionParameterForm/)
assert.match(queryEditor, /queryParameterReference\(currentQueryLanguage\.value, parameter\?\.name\)/)
assert.match(queryEditor, /editorRef\.value\?\.getSelection\(\)/)
assert.match(queryEditor, /v-loading="loadingSampleQuery"/)
assert.match(queryEditor, /:aria-busy="loadingSampleQuery"/)

console.log('query target switch tests passed')


function submissionHarness(api = {}) {
  const notices = []
  const calls = []
  const ref = value => ({ value })
  const state = {
    loadingSampleQuery: ref(false), executing: ref(false), editorRef: ref(null),
    selectedTarget: ref({}), selectedEngineId: ref(11), queryContent: ref('SELECT id FROM public.customers'),
    queryExecutionTargetLocator: ref(''), currentQueryLanguage: ref('sql'), queryParameters: ref([]), queryParameterPayload: value => value,
    protectionChoices: ref([]), exportProtectionChoices: ref([]), executionInputsComplete: () => true,
    executionRequestSequence: 0, executionResult: ref(null), resultViewMode: ref('table'),
    executionParameterDialogVisible: ref(false), announcement: ref(''), queryAnalysis: ref(null), diagnosticAnalysis: ref(null),
    preflightQuery: async () => ({ allowed: true, diagnostics: [] }),
    createExecution: async body => { calls.push(body); return { execution_id: 'old-execution' } },
    getQueryResult: async () => ({ execution_id: 'old-execution', status: 'success', rows_count: 1 }),
    queryResultFromExecution: value => value, plaintextRefresh: { schedule: value => calls.push(['schedule', value.execution_id]) },
    hasGraphData: ref(false), isTerminalExecutionStatus: () => true,
    ElMessage: { error: value => notices.push(value), success: value => notices.push(value), warning: value => notices.push(value) },
    ElMessageBox: { confirm: async () => {} }, queryWarningMessage: value => value, t: key => key,
    ...api
  }
  state.clearResult = () => { state.executionResult.value = null }
  const method = queryEditor.slice(queryEditor.indexOf('const submitQuery = async'), queryEditor.indexOf('const formatQuery ='))
  const submit = vm.runInContext(`${method}\nsubmitQuery`, vm.createContext(state))
  const replace = () => {
    state.executionRequestSequence++
    state.executionResult.value = { execution_id: 'current-execution', status: 'failed', error: 'current protection refusal', rows: [] }
    state.announcement.value = 'current protection refusal'
  }
  return { state, calls, notices, submit, replace }
}

for (const stage of ['preflightQuery', 'getQueryResult']) {
  test(`a stale ${stage} failure cannot replace the current protection refusal`, async () => {
    let rejectOld
    let started
    const entered = new Promise(resolve => { started = resolve })
    const h = submissionHarness({ [stage]: () => { started(); return new Promise((resolve, reject) => { rejectOld = reject }) } })
    const old = h.submit()
    await Promise.race([entered, old.then(() => { throw Error('submission ended before the expected API boundary') })])
    h.replace()
    const before = JSON.stringify([h.state.executionResult.value, h.state.announcement.value, h.notices])
    rejectOld({ response: { data: { error: 'obsolete protection refusal' } } })
    await old
    assert.equal(JSON.stringify([h.state.executionResult.value, h.state.announcement.value, h.notices]), before)
  })
}

test('an obsolete create-execution receipt cannot publish its pending identity', async () => {
  let resolveOld
  let started
  const entered = new Promise(resolve => { started = resolve })
  const h = submissionHarness({ createExecution: () => { started(); return new Promise(resolve => { resolveOld = resolve }) } })
  const old = h.submit()
  await Promise.race([entered, old.then(() => { throw Error('submission ended before the expected API boundary') })])
  h.replace()
  const before = JSON.stringify([h.state.executionResult.value, h.state.announcement.value, h.notices])
  resolveOld({ execution_id: 'obsolete-execution' })
  await old
  assert.equal(JSON.stringify([h.state.executionResult.value, h.state.announcement.value, h.notices]), before)
  assert.deepEqual(h.calls, [])
})

test('an abandoned high-risk confirmation cannot submit an execution after context invalidation', async () => {
  let confirm
  let started
  const entered = new Promise(resolve => { started = resolve })
  const h = submissionHarness({
    preflightQuery: async () => ({ allowed: true, diagnostics: [], requires_confirmation: true, effect: 'data_write', confirmation_token: 'fixture-confirmation' }),
    ElMessageBox: { confirm: () => { started(); return new Promise(resolve => { confirm = resolve }) } }
  })
  const old = h.submit()
  await Promise.race([entered, old.then(() => { throw Error('submission ended before the expected API boundary') })])
  h.replace()
  confirm()
  await old
  assert.deepEqual(h.calls, [])
})


function exportHarness(api = {}) {
  const calls = [], notices = []
  const ref = value => ({ value })
  const state = {
    executionResult: ref({ execution_id: 'old-query' }), resultViewMode: ref('table'),
    plaintextRefresh: { stop() {} }, fullExportDialogVisible: ref(true), fullExportFileName: ref('protected.csv'),
    fullExportSubmitting: ref(false), fullExportAbortController: null, fullExportRequests: createLatestRequestScope(),
    AbortController, announcement: ref(''), t: key => key, client: {},
    createQueryExport: async () => ({ id: 1 }),
    getQueryExport: async () => ({}),
    waitForExportSession: async () => { calls.push('poll'); return { id: 1 } },
    downloadAuthenticatedExport: async () => { calls.push('download') },
    ElMessage: { success: value => notices.push(value), error: value => notices.push(value) }, ...api
  }
  const method = queryEditor.slice(queryEditor.indexOf('const clearResult ='), queryEditor.indexOf('const queryWarningMessage ='))
  const actions = vm.runInContext(`${method}\n({submitFullExport, clearResult})`, vm.createContext(state))
  return { state, calls, notices, ...actions }
}

for (const outcome of ['success', 'failure']) test(`clearing a query discards a late export creation ${outcome}`, async () => {
  let resolve, reject
  const h = exportHarness({ createQueryExport: () => new Promise((yes, no) => { resolve = yes; reject = no }) })
  const old = h.submitFullExport({ format: 'csv', fileName: 'old.csv' })
  h.clearResult()
  assert.equal(h.state.fullExportSubmitting.value, false)
  assert.equal(h.state.fullExportDialogVisible.value, false)
  h.state.announcement.value = 'current protection refusal'
  if (outcome === 'success') resolve({ id: 1 })
  else reject(new Error('obsolete protection refusal'))
  await old
  assert.deepEqual(h.calls, [])
  assert.deepEqual(h.notices, [])
  assert.equal(h.state.announcement.value, 'current protection refusal')
})

test('old export finally cannot clear the replacement export loading state', async () => {
  const resolvers = []
  const h = exportHarness({ createQueryExport: () => new Promise(resolve => resolvers.push(resolve)) })
  const old = h.submitFullExport({ format: 'csv', fileName: 'old.csv' })
  h.clearResult()
  h.state.executionResult.value = { execution_id: 'new-query' }
  const current = h.submitFullExport({ format: 'csv', fileName: 'new.csv' })
  assert.equal(resolvers.length, 2)
  resolvers[0]({ id: 1 })
  await old
  assert.equal(h.state.fullExportSubmitting.value, true)
  assert.deepEqual(h.calls, [])
  resolvers[1]({ id: 2 })
  await current
  assert.equal(h.state.fullExportSubmitting.value, false)
  assert.deepEqual(h.calls, ['poll', 'download'])
})

test('disposed export observation ignores an in-flight creation receipt', async () => {
  let resolve
  const h = exportHarness({ createQueryExport: () => new Promise(yes => { resolve = yes }) })
  const old = h.submitFullExport({ format: 'csv', fileName: 'old.csv' })
  h.state.fullExportRequests.dispose()
  h.state.fullExportAbortController?.abort()
  h.state.fullExportAbortController = null
  resolve({ id: 1 })
  await old
  assert.deepEqual(h.calls, [])
  assert.deepEqual(h.notices, [])
})
