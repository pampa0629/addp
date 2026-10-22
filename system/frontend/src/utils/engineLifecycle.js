// Lifecycle changes use the observed resource version and never edit registration fields.
export function canDisableExtensionEngine(engine, permitted) {
  return Boolean(permitted && engine && /^[1-9]\d*$/.test(String(engine.id)) &&
    Number.isSafeInteger(engine.version) && engine.version > 0 &&
    engine.engine_origin === 'extension' && engine.lifecycle_state === 'active')
}

export function disabledEngineReadbackMatches(target, detail, listed) {
  return [detail, listed].every(engine => engine && String(engine.id) === String(target.id) &&
    engine.engine_origin === 'extension' && engine.lifecycle_state === 'disabled' &&
    Number.isSafeInteger(engine.version) && engine.version >= target.version)
}
