const stringValue = (value) => String(value || '')

export function authorizationRequestId(query) {
  const requestId = stringValue(query.request_id).trim()
  if (!requestId) {
    throw new Error('oauth_authorization_request_missing')
  }
  return requestId
}

export function authorizationDecisionRequest(requestId, decision) {
  if (decision !== 'approved' && decision !== 'rejected') {
    throw new Error('oauth_authorization_decision_invalid')
  }
  return {
    request_id: stringValue(requestId),
    decision
  }
}

export function redirectToAuthorizationResult(result, assign) {
  if (!result?.redirect_url) {
    throw new Error('oauth_redirect_url_missing')
  }
  assign(result.redirect_url)
}
