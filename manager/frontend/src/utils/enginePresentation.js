export function normalizeEngineCatalog(response) {
  const payload = response?.data?.data ?? response?.data ?? response
  return Array.isArray(payload) ? payload : []
}

export function resolveEngineName(engines, engineId) {
  const id = Number(engineId || 0)
  if (!id) return ''
  const engine = (engines || []).find((item) => Number(item?.id) === id)
  return String(engine?.name || '').trim()
}
