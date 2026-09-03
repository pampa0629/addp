export const STANDARD_SCOPE_PLATFORM = 'platform'
export const STANDARD_SCOPE_TENANT_COMMON = 'tenant_common'
export const STANDARD_SCOPE_DOMAIN = 'domain'

export const EDITABLE_STANDARD_SCOPES = [
  STANDARD_SCOPE_TENANT_COMMON,
  STANDARD_SCOPE_DOMAIN
]

export function requiresOwnerDomain(scopeType) {
  return scopeType === STANDARD_SCOPE_DOMAIN
}

export function buildStandardOwnership(scopeType, ownerDomainId) {
  return {
    scope_type: scopeType,
    owner_domain_id: requiresOwnerDomain(scopeType) ? (ownerDomainId ?? null) : null
  }
}

export function standardScopeLabelKey(scopeType) {
  return `standard.common.scopeValue.${scopeType}`
}
