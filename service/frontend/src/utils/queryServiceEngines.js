import {
  queryFederationCapability,
  supportsQueryLanguage
} from '@addp/common-frontend/basic/src/utils/engineCapabilities.mjs'

const supportsSQL = engine => supportsQueryLanguage(engine, 'sql')

const supportsFederation = engine => supportsSQL(engine) && queryFederationCapability(engine) !== null

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
  return String(selection?.resource?.representation || '').trim().toLowerCase() === 'encoded'
}
