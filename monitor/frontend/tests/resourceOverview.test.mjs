import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { resolveOverviewRoute, validateResourceOverview } from '../src/utils/resourceOverview.js'
import { overviewResponse } from '../e2e/resource-overview-response.js'

test('overview validates full server coverage, stable TOP5 and valid zero', () => {
  const body = overviewResponse()
  assert.equal(validateResourceOverview(body), body)
  assert.equal(body.rankings[0].top[4].value, 0)
  body.rankings[1].top[0].node = Object.fromEntries(Object.entries(body.rankings[1].top[0].node).reverse())
  assert.equal(validateResourceOverview(body), body)
  for (const mutate of [value => { value.inventory.managed = 20 }, value => { value.rankings[0].coverage.expected = 20 }, value => { value.rankings[0].top.reverse() }, value => { value.rankings[0].top[0].sampled_at = '2026-10-12T23:58:00Z' }, value => { value.rankings[0].top[0].node.enabled = false }, value => { value.rankings[0].top[0].value = null }, value => { value.rankings[0].top[1] = value.rankings[0].top[0] }, value => { value.rankings[0].coverage.complete = true }, value => { value.rankings[0].window_seconds = 0 }, value => { value.rankings[1].top[0].node.version++ }]) {
    const malformed = overviewResponse(); mutate(malformed)
    assert.throws(() => validateResourceOverview(malformed), /invalid_resource_response/)
  }
})
test('overview has only the canonical refresh parameter', () => {
  assert.deepEqual(resolveOverviewRoute({}).query, {})
  assert.equal(resolveOverviewRoute({}).refresh, '15')
  assert.deepEqual(resolveOverviewRoute({ refresh: 'off' }).query, { refresh: 'off' })
  for (const query of [{ refresh: ['10', '30'] }, { refresh: '' }, { refresh: '15' }, { page: '2' }, { node_id: 'private' }]) assert.equal(resolveOverviewRoute(query).changed, true)
})
test('overview reuses shared refresh, cancellation and formatting, requesting only one aggregate', () => {
  const page = readFileSync(new URL('../src/views/ResourceOverview.vue', import.meta.url), 'utf8')
  for (const helper of ['createAutoRefresh', 'createLatestRequestScope', 'formatFieldPresentationValue', 'AutoRefreshControl']) assert.ok(page.includes(helper))
  assert.ok(!page.includes('setInterval(') && !page.includes('setTimeout('))
  const api = readFileSync(new URL('../src/api/resourceOverview.js', import.meta.url), 'utf8')
  assert.ok(api.includes('/monitor/platform/resource_overview'))
  assert.ok(!api.includes('resource_summaries') && !api.includes('host_nodes'))
})
