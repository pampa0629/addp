export const ENGINE_LIFECYCLE_ACTIVE = 'active'
export const ENGINE_CONNECTION_ONLINE = 'online'

export const engineSelectionState = (engine = null) => {
  if (!engine) return 'missing'
  const lifecycle = String(engine.lifecycle_state || '').toLowerCase()
  if (lifecycle !== ENGINE_LIFECYCLE_ACTIVE) return lifecycle || 'disabled'

  const connection = String(engine.connection_status || '').toLowerCase()
  return connection === ENGINE_CONNECTION_ONLINE ? 'available' : (connection || 'unknown')
}

export const isEngineSelectable = (engine = null) => engineSelectionState(engine) === 'available'

