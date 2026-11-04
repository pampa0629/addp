export function resolveLoginRedirect(value, fallbackRoute = '/') {
  if (typeof value !== 'string' || value.includes('\\')) return fallbackRoute

  const candidate = value.trim()
  if (!candidate.startsWith('/') || candidate.startsWith('//')) return fallbackRoute

  let target
  try {
    target = new URL(candidate, 'http://addp.local')
  } catch {
    return fallbackRoute
  }
  if (target.origin !== 'http://addp.local') return fallbackRoute

  const redirect = `${target.pathname}${target.search}${target.hash}`
  // Match Vue Router's default case-insensitive, optional trailing slash rule.
  if (/^\/(?:portal\/|module-ui\/[^/]+\/)?login\/?$/i.test(target.pathname)) {
    return fallbackRoute
  }
  return redirect
}
