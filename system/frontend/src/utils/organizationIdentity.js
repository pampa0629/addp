const organizationCodePattern = /^(?:[a-z]|[a-z][a-z0-9_]{0,62}[a-z0-9])$/

export function isValidOrganizationCode(value) {
  return organizationCodePattern.test(String(value || '').trim())
}
