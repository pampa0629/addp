import test from 'node:test'
import assert from 'node:assert/strict'
import { httpAlertEvidence, validHTTPPolicy } from '../src/utils/httpRules.js'
import { platformAlertState, platformAlertQuery } from '../src/utils/platformAlerts.js'

test('HTTP rule filters and management page are independent of resource rules', () => {
  const query = { tab: 'http-rules', category: 'http_quality', http_rule_page: '2' }
  assert.deepEqual(platformAlertQuery(platformAlertState(query)), query)
  assert.deepEqual(platformAlertQuery(platformAlertState({ tab: 'http-rules', http_rule_page: '01' })), { tab: 'http-rules' })
})
test('HTTP rule thresholds use metric units and bounded complete durations', () => {
  const policy = { trigger_value: 10, recovery_value: 5, trigger_seconds: 15, recovery_seconds: 300 }
  assert.equal(validHTTPPolicy(policy, 'http.failures.percent'), true)
  for (const change of [{ trigger_value: 101 }, { recovery_value: 0 }, { recovery_value: 10 }, { trigger_seconds: 16 }, { recovery_seconds: null }, { trigger_value: Infinity }]) assert.equal(validHTTPPolicy({ ...policy, ...change }, 'http.failures.percent'), false)
  assert.equal(validHTTPPolicy({ ...policy, trigger_value: 101 }, 'http.requests.per_second'), true)
})
test('HTTP event details read frozen evidence and reject mismatched subjects or policies', () => {
  const id = '10000000-0000-4000-8000-000000000019'
  const row = { category: 'http_quality', subject_kind: 'module', subject_id: 'monitor', signal: id, http_evidence: { rule_id: id, rule_version: 1, module_name: 'monitor', role: 'backend', route: '/items/:id', method: 'GET', metric_key: 'http.failures.percent', policy: { trigger_value: 30, recovery_value: 10, trigger_seconds: 15, recovery_seconds: 15 }, query_policy_version: 1, instance_ids: ['start-one'], samples: 2, minimum: 31, maximum: 40, latest: 35, sampled_from: '2026-10-20T00:00:00Z', sampled_to: '2026-10-20T00:00:15Z', evaluated_at: '2026-10-20T00:00:16Z' } }
  assert.equal(httpAlertEvidence(row), row.http_evidence)
  assert.notEqual(httpAlertEvidence({ ...row, http_evidence: { ...row.http_evidence, query_policy_version: 0 } }), null)
  assert.equal(httpAlertEvidence({ ...row, subject_id: 'foreign' }), null)
  assert.equal(httpAlertEvidence({ ...row, http_evidence: { ...row.http_evidence, route: '/items/:id', method: 'RAW' } }), null)
  assert.equal(httpAlertEvidence({ ...row, http_evidence: { ...row.http_evidence, policy: { ...row.http_evidence.policy, recovery_value: 40 } } }), null)
})
