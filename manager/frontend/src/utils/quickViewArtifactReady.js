const cleanString = (value) => String(value || '').trim()

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, Math.max(0, Number(ms || 0))))

export function isQuickViewArtifactReady(status = {}, renderSource = '') {
  const expected = cleanString(renderSource)
  if (!expected || !status) return false
  return status.can_use_quick_view === true && cleanString(status.render_source) === expected
}

export async function waitForQuickViewArtifactReady(fetchStatus, renderSource, options = {}) {
  if (typeof fetchStatus !== 'function') {
    return { ready: false, status: null }
  }

  const expected = cleanString(renderSource)
  if (!expected) {
    return { ready: false, status: null }
  }

  const maxAttempts = Math.max(1, Number(options.maxAttempts || 10))
  const intervalMs = Math.max(0, Number(options.intervalMs ?? 1000))
  const initialDelayMs = Math.max(0, Number(options.initialDelayMs ?? 0))

  if (initialDelayMs > 0) {
    await sleep(initialDelayMs)
  }

  let lastStatus = null
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    lastStatus = await fetchStatus()
    if (isQuickViewArtifactReady(lastStatus, expected)) {
      return { ready: true, status: lastStatus }
    }
    if (attempt < maxAttempts) {
      await sleep(intervalMs)
    }
  }

  return { ready: false, status: lastStatus }
}
