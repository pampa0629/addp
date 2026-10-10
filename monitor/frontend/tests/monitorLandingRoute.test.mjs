import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))

function createMonitorRouter(permissions, contextType = 'tenant') {
  const authStore = { contextType, permissions }
  const routes = new Function('useAuthStore', 'resolveModuleLandingRoute', 'Dashboard', 'ExecutionList', 'AlertList', 'NotificationList', 'Login', 'MonitoringTargets', 'NodeResources', 'ProcessResources', `${routeTable}; return routes`)(
    () => authStore, resolveModuleLandingRoute, {}, {}, {}, {}, {}, {}, {}, {})
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

test('Monitor root resolves to the first accessible page; direct URLs keep their target', async () => {
  const executionsOnly = createMonitorRouter(['monitor.execution.read'])
  await executionsOnly.push('/')
  assert.equal(executionsOnly.currentRoute.value.path, '/executions')
  await executionsOnly.push('/executions?execution_id=42')
  assert.equal(executionsOnly.currentRoute.value.fullPath, '/executions?execution_id=42')

  const notificationsOnly = createMonitorRouter(['monitor.notification_destination.read', 'monitor.notification_delivery.read'])
  await notificationsOnly.push('/')
  assert.equal(notificationsOnly.currentRoute.value.path, '/notifications')

  const noAccess = createMonitorRouter([])
  await noAccess.push('/')
  assert.equal(noAccess.currentRoute.value.path, '/forbidden')
})

test('Platform root selects monitoring targets with both owner permissions', async () => {
  const permissions = ['monitor.monitoring_target.read', 'platform.host_node.read']
  const platform = createMonitorRouter(permissions, 'platform')
  await platform.push('/')
  assert.equal(platform.currentRoute.value.path, '/monitoring-targets')
  await platform.push('/monitoring-targets/target-id?page=2')
  assert.equal(platform.currentRoute.value.fullPath, '/monitoring-targets/target-id?page=2')
  const tenant = createMonitorRouter(permissions)
  await tenant.push('/')
  assert.equal(tenant.currentRoute.value.path, '/forbidden')
})

test('Platform root prefers resources when both read permissions are granted', async () => {
  const router = createMonitorRouter(['platform.host_node.read', 'monitor.resource_observation.read', 'monitor.monitoring_target.read'], 'platform')
  await router.push('/')
  assert.equal(router.currentRoute.value.path, '/node-resources')
})


test('Platform service entry requires resource and module reads and never requires a host binding', async () => {
 const keys = ['platform.module.read', 'monitor.resource_observation.read']
 const router = createMonitorRouter(keys, 'platform')
 await router.push('/')
 assert.equal(router.currentRoute.value.path, '/service-resources')
 for (const [context, permissions] of [['tenant', keys], ['platform', keys.slice(0, 1)], ['platform', keys.slice(1)]]) {
  const denied = createMonitorRouter(permissions, context)
  await denied.push('/')
  assert.equal(denied.currentRoute.value.path, '/forbidden')
 }
})
