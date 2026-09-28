import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveModuleLandingRoute } from '../src/authorization/consoleRouteAccess.js'

test('module landing uses the same tenant page access as Console', () => {
  const paths = ['/dashboard', '/executions', '/alerts', '/notifications']
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.statistics.read', 'monitor.execution.read']), '/dashboard')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.execution.read']), '/executions')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.alert_rule.read']), '/forbidden')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.alert_rule.read', 'monitor.alert_incident.read']), '/alerts')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.notification_destination.read', 'monitor.notification_delivery.read']), '/notifications')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'platform', ['monitor.statistics.read']), '/forbidden')
})
