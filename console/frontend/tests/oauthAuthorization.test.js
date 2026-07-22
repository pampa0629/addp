import { describe, expect, it, vi } from 'vitest'

import {
  authorizationDecisionRequest,
  redirectToAuthorizationResult
} from '../src/oauth/authorization'

describe('OAuth authorization decision', () => {
  const query = {
    client_id: 'addp-cli',
    redirect_uri: 'http://127.0.0.1:43123/callback',
    scope: 'addp.api',
    state: 'state-1',
    code_challenge: 'challenge',
    code_challenge_method: 'S256'
  }

  it('builds one canonical request for approval and rejection', () => {
    expect(authorizationDecisionRequest(query, 'approved')).toEqual({
      client_id: 'addp-cli',
      redirect_uri: 'http://127.0.0.1:43123/callback',
      scope: 'addp.api',
      state: 'state-1',
      code_challenge: 'challenge',
      code_challenge_method: 'S256',
      decision: 'approved'
    })
    expect(authorizationDecisionRequest(query, 'rejected').decision).toBe('rejected')
  })

  it('redirects using the already-unwrapped API response', () => {
    const assign = vi.fn()

    redirectToAuthorizationResult(
      { redirect_url: 'http://127.0.0.1:43123/callback?code=addp_ac_test&state=state-1' },
      assign
    )

    expect(assign).toHaveBeenCalledWith(
      'http://127.0.0.1:43123/callback?code=addp_ac_test&state=state-1'
    )
  })

  it('rejects a response without a server-validated redirect URL', () => {
    expect(() => redirectToAuthorizationResult({}, vi.fn())).toThrow('oauth_redirect_url_missing')
  })
})
