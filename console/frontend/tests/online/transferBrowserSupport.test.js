import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { isAnonymousRefreshConsoleError, matchesRedirectURL, isReadonlyTaskConfigCheckRequest } from '../../e2e/online/transfer-browser-support.js'

const base = 'http://addp.invalid'

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
