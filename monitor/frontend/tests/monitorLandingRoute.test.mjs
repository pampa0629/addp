import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))

function createMonitorRouter(permissions) {
  const authStore = { contextType: 'tenant', permissions }
  const routes = new Function('useAuthStore', 'resolveModuleLandingRoute', 'Dashboard', 'ExecutionList', 'AlertList', 'NotificationList', 'Login', `${routeTable}; return routes`)(
    () => authStore, resolveModuleLandingRoute, {}, {}, {}, {}, {})
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
