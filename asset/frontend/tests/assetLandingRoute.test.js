import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))

function createAssetRouter(permissions, contextType = 'tenant') {
  const authStore = { contextType, permissions }
  const routes = new Function('Layout', 'Login', 'useAuthStore', 'resolveModuleLandingRoute', `${routeTable}; return routes`)(
    {}, {}, () => authStore, resolveModuleLandingRoute)
  for (const child of routes[1].children) {
    if (child.component) child.component = {}
  }
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

describe('Asset standalone landing', () => {
  it('selects an accessible page in sidebar order while preserving direct routes', async () => {
    const catalogOnly = createAssetRouter(['asset.management.read', 'asset.category.read'])
    await catalogOnly.push('/')
    expect(catalogOnly.currentRoute.value.path).toBe('/categories')
    await catalogOnly.push('/categories?focus=8')
    expect(catalogOnly.currentRoute.value.fullPath).toBe('/categories?focus=8')

    const typeOnly = createAssetRouter(['asset.management.read', 'asset.entry.read'])
    await typeOnly.push('/')
    expect(typeOnly.currentRoute.value.path).toBe('/type-definitions')

    const applicationsOnly = createAssetRouter(['asset.management.read', 'asset.application.read'])
    await applicationsOnly.push('/')
    expect(applicationsOnly.currentRoute.value.path).toBe('/applications')

    const none = createAssetRouter(['asset.management.read'])
    await none.push('/')
    expect(none.currentRoute.value.path).toBe('/forbidden')

    const wrongContext = createAssetRouter(['asset.management.read', 'asset.entry.read'], 'platform')
    await wrongContext.push('/')
    expect(wrongContext.currentRoute.value.path).toBe('/forbidden')
  })
})
