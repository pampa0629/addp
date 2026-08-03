const OBJECT_TABLE_ENGINE_TYPES = new Set(['minio', 's3'])

const queryCapability = engine => {
  let capabilities = engine?.capabilities
  if (typeof capabilities === 'string') {
    try {
      capabilities = JSON.parse(capabilities)
    } catch {
      return null
    }
  }
  return capabilities?.compute?.query || null
}

const supportsSQL = engine => {
  const query = queryCapability(engine)
  return query?.supported === true && (query.languages || []).some(language => String(language).toLowerCase() === 'sql')
}

const supportsFederation = engine => supportsSQL(engine) && queryCapability(engine)?.federation?.supported === true

export function federatedQueryRuntimes(engines) {
  return (engines || []).filter(engine => engine?.lifecycle_state === 'active' && supportsFederation(engine))
}

export function queryServiceExecutionEngines(engines) {
  return (engines || []).filter(engine => engine?.lifecycle_state === 'active' && supportsSQL(engine))
}

export function applySQLExecutionEngine(form, selectedEngineID, engines) {
  const selected = (engines || []).find(engine => Number(engine.id) === Number(selectedEngineID))
  form.execution_engine_id = selected?.id || null
  if (supportsFederation(selected)) {
    form.engine_id = null
    form.runtime_engine_id = selected.id
    return
  }
  form.engine_id = selected?.id || null
  form.runtime_engine_id = null
}

export function tableSelectionUsesRuntime(selection) {
  const engineType = String(selection?.display?.engine_type || selection?.raw?.engine?.engine_type || '').trim().toLowerCase()
  return OBJECT_TABLE_ENGINE_TYPES.has(engineType)
}
