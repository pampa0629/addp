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
  if (redirect === '/login' || redirect.startsWith('/login?') || redirect.startsWith('/login#')) {
    return fallbackRoute
  }
  return redirect
}
