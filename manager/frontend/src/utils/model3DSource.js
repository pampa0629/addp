import { saveBlobDownload } from '@addp/common-frontend'

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
  const response = await fetchModelResponse(url, authenticatedFetch, { origin, signal, language })
  const bytes = await response.arrayBuffer()
  if (signal?.aborted) throw new DOMException('', 'AbortError')
  return bytes
}

async function fetchModelResponse(url, authenticatedFetch, { origin, signal, language }) {
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
  return response
}

// The Manager attachment endpoint emits one canonical RFC 6266 filename.
// Decode that name exactly; never substitute a display label or a guessed charset.
export function modelDownloadFileName(header) {
  if (/[^\x20-\x7e]/.test(header || '')) throw new Error('')
  const match = /^attachment; filename(\*)?=(.*)$/i.exec(header || '')
  if (!match) throw new Error('')
  let name
  if (match[1]) {
    if (!/^UTF-8''/i.test(match[2]) || /[^A-Za-z0-9!#$&+.^_`|~%\-]/.test(match[2].slice(7))) throw new Error('')
    name = decodeURIComponent(match[2].slice(7))
  } else if (/^"(?:[^"\\]|\\["\\])*"$/.test(match[2])) {
    name = match[2].slice(1, -1).replace(/\\(["\\])/g, '$1')
  } else {
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(match[2])) throw new Error('')
    name = match[2]
  }
  if (!name || name === '.' || name === '..' || /[\x00-\x1f\x7f/\\]/.test(name)) throw new Error('')
  return name
}

export async function downloadModel3DSource(url, authenticatedFetch, options = {}) {
  const parsed = new URL(url, options.origin)
  if (parsed.origin !== new URL(options.origin).origin || parsed.username || parsed.password || parsed.hash ||
      parsed.pathname !== '/api/v1/manager/downloads/file' ||
      [...parsed.searchParams.keys()].length !== 1 || !parsed.searchParams.get('locator')) throw new Error('')
  const response = await fetchModelResponse(url, authenticatedFetch, options)
  if (response.status !== 200) throw new Error('')
  const fileName = modelDownloadFileName(response.headers.get('Content-Disposition'))
  const blob = await response.blob()
  if (!blob.size) throw new Error('')
  if (options.signal?.aborted) throw new DOMException('', 'AbortError')
  saveBlobDownload(blob, fileName)
}
