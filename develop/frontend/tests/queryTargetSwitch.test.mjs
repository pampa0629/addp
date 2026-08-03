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

const applyQueryTargetSource = queryEditor.slice(
  queryEditor.indexOf('const applyQueryTarget = async'),
  queryEditor.indexOf('// 保存任务')
)
const executeQuerySource = queryEditor.slice(
  queryEditor.indexOf('const executeQuery = async'),
  queryEditor.indexOf('// 格式化 SQL')
)

assert.ok(
  applyQueryTargetSource.indexOf("queryContent.value = ''") < applyQueryTargetSource.indexOf('await get'),
  '切换查询目标后必须在请求样例前清空上一个目标的查询内容'
)
assert.match(executeQuerySource, /if \(loadingSampleQuery\.value\) return/)
assert.match(applyQueryTargetSource, /sampleRequests\.isCurrent\(request, selectedQueryTarget\.value\)/)
assert.match(queryEditor, /:disabled="loadingSampleQuery \|\| !selectedQueryTarget \|\| !queryContent"/)
assert.match(queryEditor, /v-loading="loadingSampleQuery"/)
assert.match(queryEditor, /:aria-busy="loadingSampleQuery"/)

console.log('query target switch tests passed')
