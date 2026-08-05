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
  queryEditor.indexOf('const onQueryTargetChange = async'),
  queryEditor.indexOf('const executeQuery = async')
)
const executeQuerySource = queryEditor.slice(
  queryEditor.indexOf('const executeQuery = async'),
  queryEditor.indexOf('const formatQuery =')
)

assert.doesNotMatch(switchTargetSource, /queryContent\.value = ''/)
assert.match(switchTargetSource, /if \(!queryContent\.value\.trim\(\)\)/)
assert.match(executeQuerySource, /if \(loadingSampleQuery\.value \|\| executing\.value\) return/)
assert.match(queryEditor, /sampleRequests\.isCurrent\(request, selectedQueryTarget\.value\)/)
assert.match(queryEditor, /createExecution\(\{/)
assert.match(queryEditor, /editorRef\.value\?\.getSelection\(\)/)
assert.match(queryEditor, /v-loading="loadingSampleQuery"/)
assert.match(queryEditor, /:aria-busy="loadingSampleQuery"/)

console.log('query target switch tests passed')
