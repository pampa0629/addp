export function contextOptionKey(option) {
  if (option?.type === 'platform') return 'platform'
  if (option?.type === 'tenant' && option.tenant_membership_id) {
    return `tenant:${option.tenant_membership_id}`
  }
  throw new Error('oauth_context_option_invalid')
}

export function contextChoice(option) {
  if (option?.type === 'platform') return { type: 'platform' }
  if (option?.type === 'tenant' && option.tenant_membership_id) {
    return { type: 'tenant', tenant_membership_id: String(option.tenant_membership_id) }
  }
  throw new Error('oauth_context_option_invalid')
}

export function currentContextOption(options) {
  return (options || []).find((option) => option.current) || null
}
