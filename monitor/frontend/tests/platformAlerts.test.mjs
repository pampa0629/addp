import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { platformAlertState, platformAlertQuery } from '../src/utils/platformAlerts.js'
test('platform alert state retains only bounded canonical management filters', () => {
 const query = { tab: 'notifications', category: 'resource', status: 'resolved', subject_id: 'host-id', page: '2', delivery_page: '3' }
 assert.deepEqual(platformAlertQuery(platformAlertState(query)), query)
 for (const query of [{ tab: ['notifications'], page: '-1', delivery_page: '01', category: 'tenant', status: 'secret', secret: 'private' }, { page: '10001', subject_id: '\nunsafe' }]) {
  assert.deepEqual(platformAlertQuery(platformAlertState(query)), {})
 }
 assert.deepEqual(platformAlertQuery(platformAlertState({ status: 'all' })), { status: 'all' })
})
test('System has no second incident or notification management owner', () => {
 const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
 const diagnostics = read('../../../system/frontend/src/components/ModuleLogPipeline.vue')
 const client = read('../../../system/frontend/src/api/logPipeline.js')
 assert.match(diagnostics, /openConsoleRoute\('\/monitor\/platform-alerts/)
 for (const source of [diagnostics, client]) assert.doesNotMatch(source, /log-notification-|alert-notification-|acknowledge\(|retryDelivery\(|destinationDraft|pipeline-incidents/)
 assert.doesNotMatch(read('../src/api/platformAlerts.js'), /log-notification-|log-pipeline\/incidents/)
})
test('resource rules retain a canonical independent page and reuse the only host selector', () => {
 assert.deepEqual(platformAlertQuery(platformAlertState({ tab: 'rules', rule_page: '3' })), { tab: 'rules', rule_page: '3' })
 assert.deepEqual(platformAlertQuery(platformAlertState({ tab: 'rules', rule_page: '01' })), { tab: 'rules' })
 const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
 for (const path of ['../src/views/MonitoringTargets.vue','../src/components/ResourceAlertRules.vue']) {
  assert.match(read(path), /<HostNodeSelect/u)
  assert.doesNotMatch(read(path), /remote-method="searchNodes"/u)
 }
 assert.match(read('../src/components/HostNodeSelect.vue'), /platform\.host_node\.read/u)
})
