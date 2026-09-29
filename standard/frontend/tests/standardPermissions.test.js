import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'

import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import {
  STANDARD_PERMISSION_RESOURCES,
  buildStandardPermission
} from '../src/utils/standardPermissions'

describe('Standard permissions', () => {
  it('builds canonical permission keys for every Standard resource', () => {
    expect(STANDARD_PERMISSION_RESOURCES.map(resource => buildStandardPermission(resource, 'update'))).toEqual([
      'standard.code_set.update',
      'standard.document.update',
      'standard.domain.update',
      'standard.element.update',
      'standard.glossary.update',
      'standard.metric.update',
      'standard.unit.update'
    ])
  })

  it('rejects aliases and unknown resources instead of creating compatibility keys', () => {
    expect(() => buildStandardPermission('code-set', 'read')).toThrow('Unknown Standard permission resource')
    expect(() => buildStandardPermission('dimension', 'read')).toThrow('Unknown Standard permission resource')
  })
})

const routerSource = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = routerSource.slice(routerSource.indexOf('const routes ='), routerSource.indexOf('const router ='))

function createStandardRouter(permissions, contextType = 'tenant') {
  const authStore = { contextType, permissions }
  const routes = new Function('Layout', 'Login', 'useAuthStore', 'resolveModuleLandingRoute', `${routeTable}; return routes`)(
    {}, {}, () => authStore, resolveModuleLandingRoute
  )
  for (const child of routes[1].children) {
    if (child.component) child.component = {}
  }
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

describe('Standard standalone landing', () => {
  it('enters the first accessible Console page and preserves direct URLs', async () => {
    for (const [permissions, contextType, expected] of [
      [['standard.domain.read'], 'tenant', '/domains'],
      [['standard.unit.read'], 'tenant', '/units'],
      [['standard.glossary.read'], 'tenant', '/forbidden'],
      [['standard.metric.read', 'standard.unit.read'], 'tenant', '/units'],
      [['standard.document.read'], 'tenant', '/forbidden'],
      [[], 'tenant', '/forbidden'],
      [['standard.unit.read'], 'platform', '/forbidden']
    ]) {
      const router = createStandardRouter(permissions, contextType)
      await router.push('/')
      expect(router.currentRoute.value.path).toBe(expected)
    }

    const direct = createStandardRouter(['standard.unit.read'])
    await direct.push('/units?category=length')
    expect(direct.currentRoute.value.fullPath).toBe('/units?category=length')
  })
})
