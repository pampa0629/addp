export function buildSecurityProtectionRoute(locator) {
  const normalized = String(locator || '').trim()
  if (!normalized) return ''
  const query = new URLSearchParams({ locator: normalized })
  return `/security/protection-enrollments?${query.toString()}`
}
