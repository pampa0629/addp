export const getStandardErrorMessage = (error, t, fallbackKey = 'standard.common.operationFailed') => {
  const message = error?.response?.data?.error
  return typeof message === 'string' && message.trim() ? message : t(fallbackKey)
}

// Download endpoints use a Blob response type even for JSON error responses.
// Normalize those errors before page-level handlers extract the structured message.
export const normalizeStandardBlobError = async (error) => {
  const data = error?.response?.data
  const headers = error?.response?.headers
  const contentType = typeof headers?.get === 'function'
    ? headers.get('content-type') || ''
    : headers?.['content-type'] || headers?.['Content-Type'] || ''
  if (!data || typeof data.text !== 'function' || !contentType.includes('json')) return error

  try {
    const payload = JSON.parse(await data.text())
    if (payload && typeof payload === 'object') error.response.data = payload
  } catch {
    // Keep the original response so the normal fallback message is used.
  }
  return error
}

export const isCanceledInteraction = error => error === 'cancel' || error === 'close'
