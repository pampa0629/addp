import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveServiceLandingRoute } from '../src/router/serviceLandingRoute.mjs'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))

function createServiceRouter(permissions) {
  const authStore = { contextType: 'tenant', permissions }
  const routes = new Function('Layout', 'useAuthStore', 'resolveServiceLandingRoute', `${routeTable}; return routes`)(
    {}, () => authStore, resolveServiceLandingRoute)
  routes[0].component = {}
  routes[1].component = {}
  for (const child of routes[1].children) {
    if (child.component) child.component = {}
  }
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

test('Service root opens a permitted page and preserves direct registered-service navigation', async () => {
  const registeredOnly = createServiceRouter(['service.external_registration.read'])
  await registeredOnly.push('/')
  assert.equal(registeredOnly.currentRoute.value.path, '/services')
  await registeredOnly.push('/services/42')
  assert.equal(registeredOnly.currentRoute.value.name, 'RegisteredServiceDetail')

  const definitionOnly = createServiceRouter(['service.definition.read'])
  await definitionOnly.push('/')
  assert.equal(definitionOnly.currentRoute.value.path, '/query-services')

  const noAccess = createServiceRouter([])
  await noAccess.push('/')
  assert.equal(noAccess.currentRoute.value.path, '/forbidden')
})
