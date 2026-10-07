import assert from 'node:assert/strict'
import test from 'node:test'
import { allowsConsoleRoute, resolveModuleLandingRoute } from '../src/authorization/consoleRouteAccess.js'

test('personal center has one self-service route in tenant and platform contexts', () => {
  for (const context of ['tenant', 'platform']) {
    assert.equal(allowsConsoleRoute('/system/account', context, []), true)
    assert.equal(allowsConsoleRoute('/system/account?tab=organization', context, []), true)
    assert.equal(allowsConsoleRoute('/system/account?tab=security', context, []), true)
    assert.equal(allowsConsoleRoute('/system/account/security', context, []), false)
    assert.equal(allowsConsoleRoute('/system/iam/organization', context, []), false)
  }
})

test('node resource list and detail require both Platform owner permissions', () => {
  const permissions = ['monitor.resource_observation.read', 'platform.host_node.read']
  for (const route of ['/monitor/node-resources', '/monitor/node-resources/node-id']) {
    assert.equal(allowsConsoleRoute(route, 'platform', permissions), true)
    assert.equal(allowsConsoleRoute(route, 'tenant', permissions), false)
    for (const permission of permissions) assert.equal(allowsConsoleRoute(route, 'platform', [permission]), false)
  }
})

test('module landing uses the same tenant page access as Console', () => {
  const paths = ['/dashboard', '/executions', '/alerts', '/notifications']
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.statistics.read', 'monitor.execution.read']), '/dashboard')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.execution.read']), '/executions')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.alert_rule.read']), '/forbidden')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.alert_rule.read', 'monitor.alert_incident.read']), '/alerts')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'tenant', ['monitor.notification_destination.read', 'monitor.notification_delivery.read']), '/notifications')
  assert.equal(resolveModuleLandingRoute('/monitor', paths, 'platform', ['monitor.statistics.read']), '/forbidden')
})

test('Transfer standalone root selects the first accessible page', () => {
  const paths = ['/tasks', '/tasks/create']
  for (const [permissions, landing] of [
    [['transfer.task.read'], '/tasks'],
    [['transfer.task.create', 'meta.catalog.read'], '/tasks/create'],
    [['transfer.task.read', 'transfer.task.create', 'meta.catalog.read'], '/tasks'],
    [['transfer.task.create'], '/forbidden'],
    [[], '/forbidden'],
  ]) {
    assert.equal(resolveModuleLandingRoute('/transfer', paths, 'tenant', permissions), landing)
  }
})

test('platform monitoring routes require node read and independent create permission', () => {
  const permissions = ['monitor.monitoring_target.read', 'platform.host_node.read']
  for (const route of ['/monitor/monitoring-targets', '/monitor/monitoring-targets/target-id']) {
    assert.equal(allowsConsoleRoute(route, 'platform', permissions), true)
    assert.equal(allowsConsoleRoute(route, 'tenant', permissions), false)
    for (const permission of permissions) assert.equal(allowsConsoleRoute(route, 'platform', [permission]), false)
  }
  assert.equal(allowsConsoleRoute('/monitor/monitoring-targets/new', 'platform', permissions), false)
  assert.equal(allowsConsoleRoute('/monitor/monitoring-targets/new', 'platform', [...permissions, 'monitor.monitoring_target.create']), true)
})
