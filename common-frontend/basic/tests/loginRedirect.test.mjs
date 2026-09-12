import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveLoginRedirect } from '../src/utils/loginRedirect.mjs'

test('keeps an exact same-origin application scenario redirect', () => {
  assert.equal(
    resolveLoginRedirect('/data-apps/application-a?preset=changsha#summary'),
    '/data-apps/application-a?preset=changsha#summary',
  )
})

test('fails closed for external, ambiguous, repeated-login, and non-scalar redirects', () => {
  for (const redirect of [
    'https://example.com/data-apps/application-a',
    '//example.com/data-apps/application-a',
    '/\\example.com/data-apps/application-a',
    '/login?redirect=/data-apps/application-a',
    ['/data-apps/application-a'],
  ]) {
    assert.equal(resolveLoginRedirect(redirect, '/applications'), '/applications')
  }
})
