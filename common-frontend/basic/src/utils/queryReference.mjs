// Identifier rendering consumes exact owner facts. It never splits dotted names
// or cleans physical names, and never substitutes a display path for a reference.
export function queryReferenceParts(reference) {
  if (!reference || typeof reference !== 'object' || Array.isArray(reference) ||
      Object.keys(reference).length !== 1 || !Object.hasOwn(reference, 'parts') ||
      !Array.isArray(reference.parts) || !reference.parts.length || reference.parts.length > 32 ||
      reference.parts.some(part => typeof part !== 'string' || !part.length || part.includes('\0'))) return null
  return [...reference.parts]
}

export function quoteQueryIdentifier(value, capability, language) {
  const text = String(value ?? '')
  if (!text) return ''
  const quote = capability?.identifierQuotes?.[String(language || '').trim().toLowerCase()]
  if (quote !== '"' && quote !== '`') return ''
  return `${quote}${text.replaceAll(quote, quote + quote)}${quote}`
}

export function nativeCatalogSegmentText(value, capability, language) {
  const text = String(value ?? '')
  if (!text) return ''
  const normalized = String(language || '').trim().toLowerCase()
  if (['mql', 'es_dsl', 'mango'].includes(normalized)) return JSON.stringify(text)
  return quoteQueryIdentifier(text, capability, normalized)
}

export function nativeCatalogPathText(segments, capability, language) {
  if (!Array.isArray(segments) || !segments.length || segments.some(part => typeof part !== 'string' || !part)) return ''
  const normalized = String(language || '').trim().toLowerCase()
  if (['mql', 'es_dsl', 'mango'].includes(normalized)) return nativeCatalogSegmentText(segments.at(-1), capability, normalized)
  const quoted = segments.map(segment => quoteQueryIdentifier(segment, capability, normalized))
  return quoted.every(Boolean) ? quoted.join('.') : ''
}

export function renderQueryReference(reference, capability, language) {
  const parts = queryReferenceParts(reference)
  if (!parts || (String(language).toLowerCase() === 'mql' && parts.length !== 1)) return ''
  return nativeCatalogPathText(parts, capability, language)
}
