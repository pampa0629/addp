import test from 'node:test'
import assert from 'node:assert/strict'

import {
  alertRuleRouteKey,
  alertRuleTargetKey,
  buildAlertRulePayload,
  parseAlertRuleRouteKey
} from '../src/utils/alertRule.js'

test('builds an exact task identity and explicit notification routes', () => {
  const target = { module: 'transfer', task_type: 'sync', source_task_id: '43', source_task_name: 'bounded sync' }
  assert.equal(alertRuleTargetKey(target), 'transfer\u0000sync\u000043')
  assert.deepEqual(buildAlertRulePayload({
    name: ' failures ', rule_type: 'consecutive_failures', failure_threshold: 3,
    severity: 'critical', enabled: true, routes: ['webhook:7', 'email:9']
  }, target), {
    name: 'failures', module: 'transfer', task_type: 'sync', source_task_id: '43', source_task_name: 'bounded sync',
    rule_type: 'consecutive_failures', failure_threshold: 3, severity: 'critical', enabled: true,
    routes: [{ channel: 'webhook', destination_id: 7 }, { channel: 'email', destination_id: 9 }]
  })
})

test('normalizes non-threshold rules and route identities', () => {
  assert.equal(alertRuleRouteKey({ channel: 'email', destination_id: 12 }), 'email:12')
  assert.deepEqual(parseAlertRuleRouteKey('webhook:5'), { channel: 'webhook', destination_id: 5 })
  const payload = buildAlertRulePayload({
    name: 'timeout', rule_type: 'last_terminal_timeout', failure_threshold: 8,
    severity: 'warning', enabled: false, routes: []
  }, { module: 'develop', task_type: 'workflow', source_task_id: 'abc' })
  assert.equal(payload.failure_threshold, 1)
})
