const stringValue = (value) => String(value || '')

export function authorizationDecisionRequest(query, decision) {
  if (decision !== 'approved' && decision !== 'rejected') {
    throw new Error('oauth_authorization_decision_invalid')
  }
  return {
    client_id: stringValue(query.client_id),
    redirect_uri: stringValue(query.redirect_uri),
    scope: stringValue(query.scope || 'addp.api'),
    state: stringValue(query.state),
    code_challenge: stringValue(query.code_challenge),
    code_challenge_method: stringValue(query.code_challenge_method),
    decision
  }
}

export function redirectToAuthorizationResult(result, assign) {
  if (!result?.redirect_url) {
    throw new Error('oauth_redirect_url_missing')
  }
  assign(result.redirect_url)
}
