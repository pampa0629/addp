import { fetchManagerSourceResponse } from './storageSource'

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

// All model bytes use the current User at Manager's own source/result endpoint.
// Source URLs never authorize credentialed external dependencies.
export function isModel3DSourceURL(url, origin) {
  try {
    const parsed = new URL(url, origin)
    return parsed.origin === new URL(origin).origin && !parsed.username && !parsed.password && !parsed.hash &&
      !parsed.searchParams.has('ticket') &&
      (isManagedModel3DGLBURL(url, origin) ||
        (parsed.pathname === '/api/v1/manager/storage-stream' && parsed.searchParams.has('locator') && parsed.searchParams.has('storage_ref')) ||
        /^\/api\/v1\/manager\/storage-assets\/[1-9]\d*\/items\/[1-9]\d*\/.+/.test(parsed.pathname))
  } catch { return false }
}

export async function fetchModel3DSource(url, authenticatedFetch, { origin, signal, language } = {}) {
  if (!isModel3DSourceURL(url, origin)) throw new Error('')
  const response = await fetchManagerSourceResponse(url, authenticatedFetch, { origin, signal, language })
  const bytes = await response.arrayBuffer()
  if (signal?.aborted) throw new DOMException('', 'AbortError')
  return bytes
}
