const sessionPayload = response => response?.data || response

const wait = (delayMs, signal) => new Promise((resolve, reject) => {
  if (signal?.aborted) {
    reject(new DOMException('Export polling was cancelled', 'AbortError'))
    return
  }
  const onAbort = () => {
    globalThis.clearTimeout(timer)
    signal?.removeEventListener('abort', onAbort)
    reject(new DOMException('Export polling was cancelled', 'AbortError'))
  }
  const timer = globalThis.setTimeout(() => {
    signal?.removeEventListener('abort', onAbort)
    resolve()
  }, delayMs)
  signal?.addEventListener('abort', onAbort, { once: true })
})

export async function waitForExportSession(getSession, sessionId, options = {}) {
  const maxAttempts = Number(options.maxAttempts) > 0 ? Number(options.maxAttempts) : 60
  const intervalMs = Number(options.intervalMs) > 0 ? Number(options.intervalMs) : 1500
  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    if (options.signal?.aborted) throw new DOMException('Export polling was cancelled', 'AbortError')
    const session = sessionPayload(await getSession(sessionId))
    if (session?.status === 'success' && session?.download_url) return session
    if (session?.status === 'failed') {
      throw new Error(session?.error_message || options.failedMessage || 'Export failed')
    }
    await wait(intervalMs, options.signal)
  }
  throw new Error(options.timeoutMessage || 'Export timed out')
}

export function downloadFromUrl(url, fileName = 'download') {
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

// A role-specialized export requires a current User request at the actual file
// boundary. Use the normal authenticated client, never a token in the URL.
export async function downloadAuthenticatedExport(client, session, options = {}) {
  const url = new URL(session.download_url, window.location.origin)
  if (url.origin !== window.location.origin || url.search || url.hash || !/^\/api\/v1\/[a-z][a-z0-9_-]*\/exports\/[1-9][0-9]*\/file$/.test(url.pathname)) {
    throw new Error(options.failedMessage || 'Invalid export download URL')
  }
  let blob
  try { blob = await client.get(url.href, { responseType: 'blob', signal: options.signal }) }
  catch (error) {
    const body = error?.response?.data
    if (body instanceof Blob && body.size <= 65536 && body.type.includes('json')) {
      try { error.response.data = JSON.parse(await body.text()) } catch { /* Keep the original transport error. */ }
    }
    throw error
  }
  if (!(blob instanceof Blob)) throw new Error(options.failedMessage || 'Invalid export download response')
  const objectUrl = URL.createObjectURL(blob)
  try { downloadFromUrl(objectUrl, session.file_name || 'download') }
  finally { globalThis.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000) }
}
