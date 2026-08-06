import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
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
assert.match(queryEditor, /if \(bypassUnsavedRouteConfirm\.value\) return true/)
assert.match(queryEditor, /develop\.query\.clearAndSwitch/)
assert.match(queryEditor, /develop\.query\.saveAndClear/)
assert.match(queryEditor, /mode="item"/)
assert.match(queryEditor, /generateQueryTemplate/)
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
assert.match(queryEditor, /queryParameterReference\(currentQueryLanguage\.value, parameter\.name\)/)
assert.match(queryEditor, /editorRef\.value\?\.getSelection\(\)/)
assert.match(queryEditor, /v-loading="loadingSampleQuery"/)
assert.match(queryEditor, /:aria-busy="loadingSampleQuery"/)

console.log('query target switch tests passed')
