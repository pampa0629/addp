import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveLoginRedirect } from '../src/utils/loginRedirect.mjs'

test('keeps an exact same-origin application scenario redirect', () => {
  for (const target of [
    '/data-apps/application-a?preset=changsha#summary',
    '/portal/my/applications?status=effective#latest',
    '/module-ui/manager/data-explorer?locator=fixture#preview',
  ]) assert.equal(resolveLoginRedirect(target), target)
})

test('fails closed for external, ambiguous, repeated-login, and non-scalar redirects', () => {
  for (const redirect of [
    'https://example.com/data-apps/application-a',
    '//example.com/data-apps/application-a',
    '/\\example.com/data-apps/application-a',
    '/login?redirect=/data-apps/application-a',
    '/LOGIN',
    '/login/',
    '/Login/?redirect=/agent#reply',
    '/agent/../LOGIN/',
    '/portal/login?redirect=/portal/home',
    '/PORTAL/Login/',
    '/portal/home/../login',
    '/module-ui/manager/login',
    '/module-ui/workbench/LOGIN/?redirect=/data-apps/application-a',
    ['/data-apps/application-a'],
  ]) {
    assert.equal(resolveLoginRedirect(redirect, '/applications'), '/applications')
  }
})
