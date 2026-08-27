const INVITATION_SECRET_PREFIX = 'addp_ti_'

export function tenantInvitationSecret(queryValue) {
  if (typeof queryValue !== 'string') throw new Error('tenant_invitation_missing')
  const value = queryValue.trim()
  if (!value.startsWith(INVITATION_SECRET_PREFIX) || value.length === INVITATION_SECRET_PREFIX.length) {
    throw new Error('tenant_invitation_invalid')
  }
  return value
}

export function tenantInvitationRegistrationRequest(input) {
  const invitationSecret = tenantInvitationSecret(input?.invitationSecret)
  const username = String(input?.username || '').trim()
  const password = String(input?.password || '')
  const displayName = String(input?.displayName || '').trim()
  const locale = String(input?.locale || '').trim()
  if (!username || !password || !displayName) throw new Error('tenant_invitation_registration_incomplete')
  return {
    invitation_secret: invitationSecret,
    username,
    password,
    display_name: displayName,
    ...(locale ? { locale } : {})
  }
}

export function tenantInvitationSession(response) {
  const payload = response?.data || response
  if (!payload?.session?.access_token) throw new Error('tenant_invitation_session_invalid')
  return payload.session
}
