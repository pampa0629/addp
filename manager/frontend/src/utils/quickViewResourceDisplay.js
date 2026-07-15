export function normalizeQuickViewEngines(response) {
  const payload = response?.data?.data ?? response?.data ?? response
  return Array.isArray(payload) ? payload : []
}

export function quickViewEngineName(engines, engineId) {
  const id = Number(engineId || 0)
  if (!id) return ''
  const engine = (engines || []).find((item) => Number(item?.id) === id)
  return String(engine?.name || '').trim()
}

export function quickViewResourcePath(locator, parseLocator) {
  const value = String(locator || '').trim()
  if (!value) return ''
  const parsed = parseLocator?.(value)
  return parsed?.path?.length ? parsed.path.join(' / ') : ''
}

export function quickViewResourceLabel(engineName, resourcePath) {
  return [engineName, resourcePath].map((value) => String(value || '').trim()).filter(Boolean).join(' / ')
}

export function quickViewDisplayText(value, parseLocator) {
  const text = String(value || '').trim()
  if (!text) return ''
  return text.replace(/addp:\/\/engine\/\d+\/path\/[^\s]+/g, (locator) => (
    quickViewResourcePath(locator, parseLocator) || locator
  ))
}
