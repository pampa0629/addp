import assert from 'node:assert/strict'
import test from 'node:test'
import { allowsConsoleRoute, resolveModuleLandingRoute } from '../src/authorization/consoleRouteAccess.js'

test('HTTP observations require current Platform scope and both owner permissions; rule reads stay separate', () => {
  const permissions = ['monitor.http_observation.read', 'platform.module.read']
  assert.equal(allowsConsoleRoute('/monitor/http-quality', 'platform', permissions), true)
  assert.equal(allowsConsoleRoute('/monitor/http-quality', 'tenant', permissions), false)
  for (const key of permissions) assert.equal(allowsConsoleRoute('/monitor/http-quality', 'platform', [key]), false)
  assert.equal(allowsConsoleRoute('/monitor/platform-alerts', 'platform', ['monitor.http_observation.read']), false)
  assert.equal(allowsConsoleRoute('/monitor/platform-alerts', 'platform', ['monitor.platform_http_rule.read']), true)
})

test('Ontology platform inspection and Tenant modeling have disjoint entry permissions', () => {
  const paths = ['/platform/definitions', '/ontologies']
  assert.equal(resolveModuleLandingRoute('/ontology', paths, 'platform', ['ontology.platform_definition.read']), '/platform/definitions')
  assert.equal(resolveModuleLandingRoute('/ontology', paths, 'tenant', ['ontology.revision.read']), '/ontologies')
  assert.equal(resolveModuleLandingRoute('/ontology', paths, 'tenant', ['ontology.platform_definition.read']), '/forbidden')
  assert.equal(allowsConsoleRoute('/ontology/platform/definitions/transfer.task.create', 'platform', ['ontology.platform_definition.read']), true)
  assert.equal(allowsConsoleRoute('/ontology/platform/definitions/transfer.task.create', 'platform', ['ontology.platform_definition.publish']), false)
})

test('Manager artifact consumer reaches Data Explorer with its eight permissions', () => {
  const permissions = [
    'manager.data_item.read', 'manager.derived_artifact.create',
    'manager.derived_artifact.delete', 'manager.derived_artifact.read',
    'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
    'monitor.execution.read',
  ]
  assert.equal(permissions.length, 8)
  for (const route of ['/manager/data-explorer', '/manager/data-explorer?locator=source']) {
    assert.equal(allowsConsoleRoute(route, 'tenant', ['manager.data_item.read']), true)
    assert.equal(allowsConsoleRoute(route, 'tenant', permissions), true)
    assert.equal(allowsConsoleRoute(route, 'platform', permissions), false)
    assert.equal(allowsConsoleRoute(route, 'tenant', permissions.filter(key => key !== 'manager.data_item.read')), false)
    assert.equal(allowsConsoleRoute(route, 'tenant', ['manager.content.read']), false)
    assert.equal(allowsConsoleRoute(route, 'tenant', ['manager.derived_artifact.read']), false)
    assert.equal(allowsConsoleRoute(route, 'tenant', []), false)
  }
  assert.equal(resolveModuleLandingRoute('/manager', ['/data-explorer', '/tasks/quick-view'], 'tenant', permissions), '/data-explorer')
  assert.equal(resolveModuleLandingRoute('/manager', ['/data-explorer', '/tasks/quick-view'], 'tenant', ['manager.data_item.read']), '/data-explorer')
})

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

test('resource overview requires both platform owners and never becomes a Tenant entry', () => {
  const keys = ['monitor.resource_observation.read', 'platform.host_node.read']
  assert.equal(allowsConsoleRoute('/monitor/resource-overview', 'platform', keys), true)
  for (const [context, permissions] of [['tenant', keys], ['platform', keys.slice(0, 1)], ['platform', keys.slice(1)]]) {
    assert.equal(allowsConsoleRoute('/monitor/resource-overview', context, permissions), false)
  }
})

test('platform alerts and notification administration have independent entry reads', () => {
 for (const permission of ['monitor.platform_alert.read', 'monitor.platform_alert_notification.read', 'monitor.platform_resource_rule.read', 'monitor.platform_service_availability_rule.read']) {
  assert.equal(allowsConsoleRoute('/monitor/platform-alerts?tab=notifications', 'platform', [permission]), true)
  assert.equal(allowsConsoleRoute('/monitor/platform-alerts', 'tenant', [permission]), false)
 }
 for (const permission of ['monitor.log_pipeline.read', 'monitor.log_notification.read', 'monitor.platform_alert.update', 'monitor.platform_alert_notification.update', 'monitor.platform_resource_rule.create', 'monitor.platform_resource_rule.update', 'monitor.platform_resource_rule.delete']) {
  assert.equal(allowsConsoleRoute('/monitor/platform-alerts', 'platform', [permission]), false)
 }
 assert.equal(resolveModuleLandingRoute('/monitor', ['/platform-alerts', '/alerts'], 'platform', ['monitor.platform_alert.read']), '/platform-alerts')
 assert.equal(allowsConsoleRoute('/monitor/alerts', 'platform', ['monitor.platform_alert.read']), false)
})

test('measurement units have one entry in both verified contexts', () => {
  for (const context of ['platform', 'tenant']) {
    assert.equal(allowsConsoleRoute('/standard/units', context, ['standard.unit.read']), true)
    assert.equal(allowsConsoleRoute('/standard/units', context, ['standard.unit.update']), false)
  }
  assert.equal(allowsConsoleRoute('/standard/domains', 'platform', ['standard.domain.read']), false)
})
