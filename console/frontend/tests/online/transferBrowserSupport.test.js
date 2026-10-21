import { describe, expect, it } from 'vitest'
import { createServer } from 'node:http'
import { createMemoryHistory, createRouter } from 'vue-router'
import { browserAPIFromResponse, browserAPIFromNavigation, identity, isAnonymousRefreshConsoleError, matchesRedirectURL, isReadonlyTaskConfigCheckRequest } from '../../e2e/online/transfer-browser-support.js'

const base = 'http://addp.invalid'

const tenantIdentity = () => ({ principal: { id: '7', type: 'user' },
  context: { type: 'tenant', tenant_id: '2' },
  authorization: { role_assignments: [{ permissions: ['security.enrollment.read', 'meta.catalog.read'] }] } })
const browserResponse = authorization => ({ ok: () => true, request: () => ({ headers: () => ({ authorization }) }) })

async function withSessionAuthority(run) {
  const authority = { credential: 'Bearer before-navigation', payload: tenantIdentity(), requests: [] }
  const server = createServer((request, response) => {
    authority.requests.push({ method: request.method, path: request.url })
    response.setHeader('Content-Type', 'application/json')
    if (request.headers.authorization !== authority.credential) {
      response.writeHead(401)
      response.end(JSON.stringify({ error_code: 'authentication_required' }))
      return
    }
    response.end(JSON.stringify(authority.payload))
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  try {
    await run(authority, `http://127.0.0.1:${server.address().port}`)
  } finally {
    server.closeAllConnections()
    await new Promise(resolve => server.close(resolve))
  }
}

describe('current browser session API binding', () => {
  it('rebinds generation, cache reload, next model and withdrawal to each real navigation response', async () => {
    await withSessionAuthority(async (authority, gateway) => {
      const expected = identity(tenantIdentity())
      let current
      let deliver
      const page = { waitForResponse: predicate => new Promise(resolve => {
        deliver = response => { expect(predicate(response)).toBe(true); resolve(response) }
      }) }
      try {
        for (const phase of ['ifc-generation', 'ifc-cache-reload', '3ds-generation', '3ds-cache-reload', 'withdrawn-ifc', 'withdrawn-3ds']) {
          const previous = current
          current = await browserAPIFromNavigation(page, gateway, expected, async () => {
            authority.credential = `Bearer current-${phase}`
            deliver(browserResponse(authority.credential))
          }, response => response.ok())
          expect((await current.get('/api/v1/system/auth/context')).status()).toBe(200)
          if (previous) {
            try {
              expect((await previous.get('/api/v1/system/auth/context')).status()).toBe(401)
            } finally { await previous.dispose() }
          }
        }
        expect(authority.requests.every(item => item.method === 'GET' && item.path === '/api/v1/system/auth/context')).toBe(true)
      } finally { await current?.dispose() }
    })
  })

  it('fails closed if navigation switches user or tenant, or rejects the current credential', async () => {
    for (const change of [
      authority => { authority.payload.principal.id = '8' },
      authority => { authority.payload.context.tenant_id = '3' },
      authority => { authority.credential = 'Bearer unrelated' }
    ]) {
      await withSessionAuthority(async (authority, gateway) => {
        let deliver
        const page = { waitForResponse: predicate => new Promise(resolve => {
          deliver = response => { expect(predicate(response)).toBe(true); resolve(response) }
        }) }
        await expect(browserAPIFromNavigation(page, gateway, identity(tenantIdentity()), async () => {
          change(authority)
          deliver(browserResponse('Bearer before-navigation'))
        }, response => response.ok())).rejects.toThrow()
        expect(authority.requests).toHaveLength(1)
      })
    }
  })

  it('rejects the pre-navigation credential and binds the actual new browser request', async () => {
    await withSessionAuthority(async (authority, gateway) => {
      const expected = identity(tenantIdentity())
      const previous = await browserAPIFromResponse(browserResponse(authority.credential), gateway, expected)
      authority.credential = 'Bearer after-navigation'
      try {
        expect((await previous.get('/api/v1/system/auth/context')).status()).toBe(401)
      } finally {
        await previous.dispose()
      }
      const current = await browserAPIFromResponse(browserResponse(authority.credential), gateway, expected)
      try {
        expect((await current.get('/api/v1/system/auth/context')).status()).toBe(200)
        expect(authority.requests.every(item => item.method === 'GET' && item.path === '/api/v1/system/auth/context')).toBe(true)
      } finally {
        await current.dispose()
      }
    })
  })

  it('refuses another user, tenant, context, principal type or permission set', async () => {
    for (const change of [
      payload => { payload.principal.id = '8' },
      payload => { payload.context.tenant_id = '3' },
      payload => { payload.context.type = 'platform' },
      payload => { payload.principal.type = 'service' },
      payload => { payload.authorization.role_assignments[0].permissions = ['security.enrollment.read'] }
    ]) {
      await withSessionAuthority(async (authority, gateway) => {
        change(authority.payload)
        await expect(browserAPIFromResponse(browserResponse(authority.credential), gateway, identity(tenantIdentity()))).rejects.toThrow()
        expect(authority.requests).toHaveLength(1)
      })
    }
  })

  it('does not substitute a fixture credential for a missing or rejected browser credential', async () => {
    await withSessionAuthority(async (authority, gateway) => {
      for (const value of [undefined, '', 'Basic unrelated']) {
        await expect(browserAPIFromResponse(browserResponse(value), gateway, identity(tenantIdentity()))).rejects.toThrow()
      }
      expect(authority.requests).toEqual([])
      await expect(browserAPIFromResponse(browserResponse('Bearer obsolete'), gateway, identity(tenantIdentity()))).rejects.toThrow(/401/)
      expect(authority.requests).toHaveLength(1)
    })
  })
})

describe('read-only task configuration check safety guard', () => {
  it('allows only real checks, session refresh and declared persisted facts', () => {
    for (const [method, path] of [
      ['POST', '/api/v1/transfer/task-config-checks'], ['POST', '/api/v1/system/refresh'],
      ['GET', '/api/v1/system/auth/context'], ['GET', '/api/v1/meta/engines'],
      ['GET', '/api/v1/meta/resource-tree/7/search'], ['GET', '/api/v1/meta/items/9/fields'],
      ['GET', '/api/v1/transfer/capabilities'], ['GET', '/api/v1/transfer/task-definitions'],
      ['GET', '/api/v1/transfer/task-definitions/8/executions']
    ]) expect(isReadonlyTaskConfigCheckRequest(method, path), `${method} ${path}`).toBe(true)
  })
  it('rejects task mutations, execution, scanning and business reads', () => {
    for (const [method, path] of [
      ['POST', '/api/v1/transfer/task-definitions'], ['PUT', '/api/v1/transfer/task-definitions/8'],
      ['DELETE', '/api/v1/transfer/task-definitions/8'], ['POST', '/api/v1/transfer/task-definitions/8/start'],
      ['POST', '/api/v1/meta/scan/run/manual'], ['POST', '/api/v1/develop/queries'],
      ['GET', '/api/v1/manager/preview'], ['GET', '/api/v1/manager/items/7/preview'],
      ['GET', '/api/v1/manager/data-items/7/content'], ['GET', '/api/v1/meta/items/7/rows'],
      ['GET', '/api/v1/transfer/task-config-checks'], ['POST', '/api/v1/meta/engines']
    ]) expect(isReadonlyTaskConfigCheckRequest(method, path), `${method} ${path}`).toBe(false)
  })
})

describe('Online login target route', () => {
  it('accepts the same full locator after Vue Router serializes its query', () => {
    const locator = 'addp://2/security_fixture/customers?item_id=7&fingerprint=abc'
    const expected = new URL(`/manager/data-explorer?locator=${encodeURIComponent(locator)}`, base)
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/manager/data-explorer', name: 'DataExplorer', component: {} }
    ] })
    const actual = new URL(router.resolve({ name: 'DataExplorer', query: { locator } }).fullPath, base)
    expect(actual.search).not.toBe(expected.search)
    expect(actual.searchParams.get('locator')).toBe(locator)
    expect(matchesRedirectURL(actual, expected)).toBe(true)
  })

  it('accepts query order and encoding differences without dropping any parameter', () => {
    expect(matchesRedirectURL(new URL('/manager/data-explorer?q=a+b&locator=x', base),
      new URL('/manager/data-explorer?locator=x&q=a%20b', base))).toBe(true)
  })

  it('rejects another page, locator, missing or extra parameters and changed duplicate values', () => {
    const expected = new URL('/manager/data-explorer?locator=x&tag=a&tag=b', base)
    for (const path of [
      '/forbidden?locator=x&tag=a&tag=b',
      '/manager/data-explorer?locator=y&tag=a&tag=b',
      '/manager/data-explorer?tag=a&tag=b',
      '/manager/data-explorer?locator=x&tag=a&tag=b&extra=1',
      '/manager/data-explorer?locator=x&tag=b&tag=a'
    ]) expect(matchesRedirectURL(new URL(path, base), expected), path).toBe(false)
  })
})

describe('anonymous session refresh diagnostics', () => {
  const message = (path, text = 'Failed to load resource: the server responded with a status of 401 (Unauthorized)', type = 'error') => ({
    type: () => type, text: () => text, location: () => ({ url: path })
  })

  it('recognizes only the native refresh 401 before business starts', () => {
    const native = message(`${base}/api/v1/system/refresh`)
    expect(isAnonymousRefreshConsoleError(native, false)).toBe(true)
    expect(isAnonymousRefreshConsoleError(native, true)).toBe(false)
    for (const other of [
      message(`${base}/api/v1/system/auth/context`), message(''),
      message(`${base}/api/v1/system/refresh`, 'application failure'),
      message(`${base}/api/v1/system/refresh`, 'Failed to load resource: the server responded with a status of 500 (Internal Server Error)'),
      message(`${base}/api/v1/system/refresh`, native.text(), 'warning')
    ]) expect(isAnonymousRefreshConsoleError(other, false)).toBe(false)
  })
})
