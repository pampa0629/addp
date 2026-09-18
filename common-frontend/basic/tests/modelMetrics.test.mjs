import test from 'node:test'
import assert from 'node:assert/strict'
import { createModelMetricAPI, publishedMetricSources } from '../src/api/modelMetrics.js'

test('Model SDK is lazy and uses the caller authenticated client and canonical routes', async () => {
  const calls = []
  const api = createModelMetricAPI({ get: async (...args) => { calls.push(args); return [] } })
  assert.equal(calls.length, 0)
  await api.list({ fact_table_id: 3 })
  await api.get(12)
  assert.deepEqual(calls, [['/model/metric-implementations', { params: { fact_table_id: 3 } }], ['/model/metric-implementations/12']])
})

test('published choices keep every exact published revision and exclude drafts and withdrawn revisions', () => {
  const items = [{ id: 3, name: 'Metric', revisions: [
    { id: 8, revision_no: 4, status: 'draft' },
    { id: 7, revision_no: 3, status: 'published' },
    { id: 6, revision_no: 2, status: 'published' },
    { id: 5, revision_no: 1, status: 'withdrawn' },
  ] }, { id: 4, name: 'Unpublished', revisions: [] }]
  assert.deepEqual(publishedMetricSources(items), [{ id: 3, name: 'Metric', revisions: [{ id: 7, revision_no: 3, supports_details: false }, { id: 6, revision_no: 2, supports_details: false }] }])
  assert.equal(items[0].revisions.length, 4)
  assert.throws(() => publishedMetricSources({ data: items }))
})
