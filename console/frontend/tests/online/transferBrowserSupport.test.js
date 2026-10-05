import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { isAnonymousRefreshConsoleError, matchesRedirectURL } from '../../e2e/online/transfer-browser-support.js'

const base = 'http://addp.invalid'

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
