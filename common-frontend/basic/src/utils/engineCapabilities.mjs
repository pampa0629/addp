export function parseEngineCapabilities(engineOrCapabilities) {
  const raw = engineOrCapabilities?.capabilities ?? engineOrCapabilities
  if (!raw) return null
  if (typeof raw === 'object' && !Array.isArray(raw)) return raw
  if (typeof raw !== 'string') return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
  } catch {
    return null
  }
}

export function engineCapabilityFamily(engine) {
  return String(parseEngineCapabilities(engine)?.engine_family || '').trim().toLowerCase()
}

export function hasDeclaredStorageCapability(engine) {
  const storage = parseEngineCapabilities(engine)?.storage
  return Boolean(storage && typeof storage === 'object' && !Array.isArray(storage))
}

export function isNativeTableStorageEngine(engine) {
  return hasDeclaredStorageCapability(engine) && engineCapabilityFamily(engine) === 'tabular'
}

export function isContentStorageEngine(engine) {
  return hasDeclaredStorageCapability(engine) && ['file', 'object'].includes(engineCapabilityFamily(engine))
}

export function supportsQueryLanguage(engine, language) {
  const query = parseEngineCapabilities(engine)?.compute?.query
  const normalizedLanguage = String(language || '').trim().toLowerCase()
  return query?.supported === true && (query.languages || [])
    .some(value => String(value || '').trim().toLowerCase() === normalizedLanguage)
}

export function queryFederationCapability(engine) {
  const federation = parseEngineCapabilities(engine)?.compute?.query?.federation
  if (federation?.supported !== true) return null
  return {
    sourceEngineTypes: new Set((federation.source_engine_types || [])
      .map(value => String(value || '').trim().toLowerCase())
      .filter(Boolean)),
    objectFormats: new Set((federation.object_formats || [])
      .map(value => String(value || '').trim().toLowerCase())
      .filter(Boolean))
  }
}
