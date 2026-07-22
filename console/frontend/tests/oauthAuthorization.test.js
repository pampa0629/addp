import { describe, expect, it, vi } from 'vitest'

import {
  authorizationDecisionRequest,
  authorizationRequestId,
  redirectToAuthorizationResult
} from '../src/oauth/authorization'

describe('OAuth authorization decision', () => {
  const query = { request_id: 'request-1' }

  it('accepts only the server-managed request id from the browser query', () => {
    expect(authorizationRequestId(query)).toBe('request-1')
    expect(() => authorizationRequestId({})).toThrow('oauth_authorization_request_missing')
  })

  it('builds one canonical decision request for approval and rejection', () => {
    expect(authorizationDecisionRequest('request-1', 'approved')).toEqual({
      request_id: 'request-1',
      decision: 'approved'
    })
    expect(authorizationDecisionRequest('request-1', 'rejected').decision).toBe('rejected')
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
