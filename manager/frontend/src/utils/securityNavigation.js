export function buildSecurityProtectionRoute(locator) {
  const normalized = String(locator || '').trim()
  if (!normalized) return ''
  const query = new URLSearchParams({ action: 'enroll', locator: normalized })
  return `/security/protection-enrollments?${query.toString()}`
}
