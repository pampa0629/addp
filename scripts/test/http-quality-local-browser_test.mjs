import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import * as support from '../../console/frontend/e2e/http-quality-page-support.mjs'

function fixture(t, { logoutStatus = 204, replayStatus = 401, authorization = 'Bearer addp_at_SECRET_SENTINEL_1234567890', fail = null } = {}) {
  const calls = []
  let predicate, resolveResponse, currentURL = 'http://nginx/monitor/http-quality'
  const request = { method: () => 'POST', headers: () => ({ authorization }) }
  const response = { url: () => 'http://nginx/api/v1/system/logout', request: () => request, status: () => logoutStatus }
  const page = {
    waitForResponse(check) {
      assert.equal(check({ ...response, url: () => 'http://foreign.invalid/api/v1/system/logout' }), false)
      assert.equal(check({ ...response, request: () => ({ ...request, method: () => 'GET' }) }), false)
      assert.equal(check({ ...response, url: () => 'http://nginx/api/v1/system/not-logout' }), false)
      predicate = check; return new Promise(resolve => { resolveResponse = resolve }) },
    locator(selector) { assert.equal(selector, '.user-dropdown'); return { async hover() { calls.push('hover') } } },
    getByRole(role, options) {
      assert.equal(role, 'menuitem'); assert.ok(options.name.test('退出登录')); assert.ok(options.name.test('Logout'))
      return { async click() {
        if (fail === 'click') throw new Error('SECRET_SENTINEL click details')
        calls.push('POST'); assert.equal(predicate(response), true)
        if (logoutStatus === 204) currentURL = 'http://nginx/login'
        resolveResponse(response)
      } }
    },
    async evaluate(fn, credential) { return fn(credential) }
  }
  t.mock.method(globalThis, 'fetch', async (path, options) => {
    calls.push('GET')
    assert.equal(options.method ?? 'GET', 'GET'); assert.equal(path, '/api/v1/system/auth/context'); assert.equal(options.headers.Authorization, authorization)
    assert.equal(options.credentials, 'omit'); assert.equal(options.redirect, 'error'); assert.equal(options.cache, 'no-store')
    if (fail === 'replay') throw new Error('SECRET_SENTINEL fetch details')
    return { status: replayStatus }
  })
  const expect = target => ({ async toHaveURL(pattern) { assert.equal(target, page); assert.match(currentURL, pattern) } })
  return { page, expect, calls }
}

test('orchestrates UI logout and exactly one old-session request without exporting credentials', async t => {
  assert.equal(typeof support.assertHTTPBrowserLogout, 'function')
  const f = fixture(t)
  assert.equal(await support.assertHTTPBrowserLogout(f.page, f.expect, 2), 2)
  assert.deepEqual(f.calls, ['hover', 'POST', 'GET'])
})

test('insufficient or invalid remaining allowance never starts logout transport', async t => {
  const f = fixture(t)
  for (const remaining of [0, 1, -1, 81, NaN, true]) {
    await assert.rejects(() => support.assertHTTPBrowserLogout(f.page, f.expect, remaining), /http_browser_logout_budget_insufficient/)
  }
  assert.deepEqual(f.calls, [])
})

test('failed logout never substitutes an anonymous rejection or sends the replay', async t => {
  for (const logoutStatus of [401, 403, 500]) {
    const f = fixture(t, { logoutStatus })
    await assert.rejects(() => support.assertHTTPBrowserLogout(f.page, f.expect, 80), /http_browser_logout/)
    assert.deepEqual(f.calls, ['hover', 'POST'])
  }
})

test('missing or malformed old credentials cannot pass and never reach the rejection request', async t => {
  for (const authorization of [undefined, '', 'Basic SECRET_SENTINEL', 'Bearer SECRET_SENTINEL\ninvalid']) {
    const f = fixture(t, { authorization: authorization === undefined ? null : authorization })
    await assert.rejects(() => support.assertHTTPBrowserLogout(f.page, f.expect, 80), error =>
      /http_browser_logout/.test(error.message) && !error.message.includes('SECRET_SENTINEL'))
    assert.deepEqual(f.calls, ['hover', 'POST'])
  }
})

test('only 401 proves old-session rejection; other statuses fail after two requests without retry', async t => {
  for (const replayStatus of [200, 204, 403, 500]) {
    const f = fixture(t, { replayStatus })
    await assert.rejects(() => support.assertHTTPBrowserLogout(f.page, f.expect, 80), /http_browser_logout/)
    assert.deepEqual(f.calls, ['hover', 'POST', 'GET'])
  }
})

test('transport errors keep a closed error code and the original attempt count', async t => {
  for (const fail of ['click', 'replay']) {
    const f = fixture(t, { fail })
    await assert.rejects(() => support.assertHTTPBrowserLogout(f.page, f.expect, 80), error =>
      /^http_browser_logout_[a-z_]+$/.test(error.message) && !error.message.includes('SECRET_SENTINEL'))
    assert.equal(f.calls.filter(value => value === 'POST' || value === 'GET').length, fail === 'click' ? 0 : 2)
  }
})

test('both formal browser phases require logout before closure and retain the original request guard', () => {
  const source = readFileSync(new URL('../../console/frontend/e2e/online/http-quality-local.spec.js', import.meta.url), 'utf8')
  assert.match(source, /await assertHTTPBrowserLogout\(page, expect, input\.request_limit - report\.api_requests\)/)
  assert.match(source, /report\.logout_api_requests = report\.api_requests - logoutStart/)
  assert.match(source, /report\.logout_api_requests !== 2/)
  assert.match(source, /report\.assertions\.browser_logout_old_session_denied = true/)
  const close = source.indexOf('await page.close()', source.indexOf('// Freeze successful counts'))
  assert.ok(source.indexOf('await assertHTTPBrowserLogout(') < close)
  assert.ok(source.indexOf('report.logout_api_requests =') > close)
  assert.match(source, /report\.api_requests\+\+/)
  for (const forbidden of ['localStorage', 'storageState', 'setExtraHTTPHeaders', 'page.request', 'request.newContext']) assert.equal(source.includes(forbidden), false)
})
