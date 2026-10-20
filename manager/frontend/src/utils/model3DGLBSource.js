export function isManagedModel3DGLBURL(url, origin) {
  try {
    const parsed = new URL(url, origin)
    return parsed.origin === new URL(origin).origin &&
      !parsed.username && !parsed.password && !parsed.hash &&
      /^\/api\/v1\/manager\/model_3d_glb\/[1-9]\d*\/content$/.test(parsed.pathname)
  } catch {
    return false
  }
}

// This owner adapter sends credentials only to Manager's own GLB endpoint.
// Converted GLBs are self-contained; their bytes need no credentialed subloads.
export async function loadManagedModel3DGLB(loader, url, authenticatedFetch, { origin, signal, language } = {}) {
  if (!isManagedModel3DGLBURL(url, origin)) throw new Error('')
  const response = await authenticatedFetch(new URL(url, origin).href, {
    method: 'GET', signal, credentials: 'omit', redirect: 'error', cache: 'no-store',
    ...(language ? { headers: { 'Accept-Language': language } } : {})
  })
  if (!response.ok) {
    const payload = await response.json().catch(() => null)
    const error = new Error(payload?.error || payload?.message || '')
    error.status = response.status
    throw error
  }
  const bytes = await response.arrayBuffer()
  if (signal?.aborted) throw new DOMException('', 'AbortError')
  return loader.parseAsync(bytes, '')
}
