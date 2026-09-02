export function saveDataDictionaryExport(blob, fileName) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export async function dataDictionaryExportFileName(blob, entryID, fallback = new Date()) {
  let generatedAt = fallback
  try {
    const snapshot = JSON.parse(await blob.text())
    const parsed = new Date(snapshot?.generated_at)
    if (!Number.isNaN(parsed.getTime())) generatedAt = parsed
  } catch {
    // The authenticated download already succeeded; keep a safe local timestamp.
  }
  const safeEntryID = String(entryID || 'entry').replace(/[^a-zA-Z0-9-]/g, '-')
  const timestamp = generatedAt.toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z')
  return `data-dictionary-${safeEntryID}-${timestamp}.json`
}

export async function normalizeDataDictionaryBlobError(error) {
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
    // Preserve the Blob response so the caller uses its localized fallback.
  }
  return error
}
