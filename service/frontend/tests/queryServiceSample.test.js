import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'

import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'

test('latest sample request wins when query engines are switched quickly', () => {
  const coordinator = createLatestRequestCoordinator()
  const first = coordinator.begin(11)
  const second = coordinator.begin(12)

  assert.equal(coordinator.isCurrent(first, 12), false)
  assert.equal(coordinator.isCurrent(second, 12), true)
})

test('query service form clears stale SQL before loading a real sample', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')
  const start = source.indexOf('const handleSQLExecutionEngineChange = async')
  const end = source.indexOf('// 方法：处理表选择', start)
  const handler = source.slice(start, end)

  assert.ok(handler.indexOf("form.sql_query = ''") < handler.indexOf('await queryServiceAPI.getQueryEngineSample'))
  assert.match(handler, /sampleRequests\.isCurrent\(request, form\.execution_engine_id\)/)
  assert.doesNotMatch(handler, /SELECT\s+1/i)
})

test('stable key selector detects the current SQL output when opened', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')

  assert.match(source, /@visible-change="handleSQLStableKeyVisibleChange"/)
  assert.match(source, /:loading="detectingSQLOutput"/)
  assert.match(source, /:filter-method="filterSQLStableKeyFields"/)
  assert.match(source, /String\(query \|\| ''\)\.trim\(\)/)
  assert.match(source, /if \(visible\)/)
  assert.match(source, /!sqlOutputContract\.value\s*&&\s*!detectingSQLOutput\.value/)
  assert.match(source, /outputContractRequests\.invalidate\(\)/)
  assert.doesNotMatch(source, /@click="detectSQLSpatialFields"/)
})

test('query service form displays the backend error before the generic HTTP error', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')
  const submitStart = source.indexOf('const handleSubmit = async')
  const submitEnd = source.indexOf('// 方法：返回列表', submitStart)
  const submitHandler = source.slice(submitStart, submitEnd)

  assert.match(submitHandler, /error\.response\?\.data\?\.error \|\| error\.message/)
})
