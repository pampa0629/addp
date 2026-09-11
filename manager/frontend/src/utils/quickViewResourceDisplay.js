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
