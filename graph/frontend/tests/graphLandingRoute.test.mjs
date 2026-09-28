import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))

function createGraphRouter(permissions) {
  const authStore = { contextType: 'tenant', permissions }
  const routes = new Function('Layout', 'useAuthStore', 'resolveModuleLandingRoute', `${routeTable}; return routes`)(
    {}, () => authStore, resolveModuleLandingRoute)
  routes[0].component = {}
  routes[1].component = {}
  for (const child of routes[1].children) {
    if (child.component) child.component = {}
  }
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

describe('Graph standalone landing', () => {
  it('enters Graph pages with their own permissions and preserves direct navigation', async () => {
    const graphOnly = createGraphRouter(['graph.graph.read'])
    await graphOnly.push('/')
    expect(graphOnly.currentRoute.value.path).toBe('/graphs')
    await graphOnly.push('/graphs/42/browse')
    expect(graphOnly.currentRoute.value.name).toBe('GraphBrowser')

    const analysisOnly = createGraphRouter(['graph.graph.read', 'graph.analysis.read'])
    await analysisOnly.push('/')
    expect(analysisOnly.currentRoute.value.path).toBe('/graphs')

    const ontologyOnly = createGraphRouter(['graph.ontology.read'])
    await ontologyOnly.push('/')
    expect(ontologyOnly.currentRoute.value.path).toBe('/ontologies')

    const noAccess = createGraphRouter([])
    await noAccess.push('/')
    expect(noAccess.currentRoute.value.path).toBe('/forbidden')
  })
})
