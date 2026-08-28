const MAX_REDIRECT_URI_LENGTH = 2048

function normalizedHostname(url) {
  return url.hostname.startsWith('[') && url.hostname.endsWith(']')
    ? url.hostname.slice(1, -1)
    : url.hostname
}

function isLoopbackIPLiteral(hostname) {
  if (hostname === '::1') return true
  const parts = hostname.split('.')
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255) && Number(parts[0]) === 127
}

export function isValidOAuthRedirectURI(value) {
  const raw = String(value || '').trim()
  if (!raw || raw.length > MAX_REDIRECT_URI_LENGTH || raw.includes('*')) return false
  try {
    const parsed = new URL(raw)
    if (!parsed.host || parsed.username || parsed.password || parsed.hash) return false
    if (normalizedHostname(parsed).toLowerCase() === 'localhost') return false
    if (parsed.protocol === 'https:') return true
    return parsed.protocol === 'http:' && isLoopbackIPLiteral(normalizedHostname(parsed))
  } catch {
    return false
  }
}

export function validateOAuthRedirectURIs(values) {
  if (!Array.isArray(values) || values.length === 0 || values.length > 10) return false
  const normalized = values.map(value => String(value || '').trim())
  return normalized.every(isValidOAuthRedirectURI) && new Set(normalized).size === normalized.length
}
