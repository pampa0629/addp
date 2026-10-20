export const ENGINE_CAPABILITY_GROUPS = ['storage', 'compute']
export const ENGINE_ORIGINS = ['general', 'extension']
export const ENGINE_LIFECYCLE_STATES = ['active', 'disabled', 'deleting', 'deleted']

export const createEngineFilters = () => ({
  capabilityGroups: [...ENGINE_CAPABILITY_GROUPS],
  engineOrigins: [...ENGINE_ORIGINS],
  registration: 'all',
  lifecycleStates: ENGINE_LIFECYCLE_STATES.filter(state => state !== 'deleted')
})

export const filterEngineList = (engines, filters) => engines.filter(engine => (
  filters.capabilityGroups.some(group => engine.capabilities?.[group] != null) &&
  filters.engineOrigins.includes(engine.engine_origin) &&
  (filters.registration === 'all' ||
    (filters.registration === 'builtin' && engine.is_builtin === true) ||
    (filters.registration === 'registered' && engine.is_builtin === false)) &&
  filters.lifecycleStates.includes(engine.lifecycle_state)
))

export const paginateEngines = (engines, page, pageSize) => {
  const start = (page - 1) * pageSize
  return engines.slice(start, start + pageSize)
}

export const ENGINE_STATUS_REFRESH_INTERVAL_MS = 10000
export const ENGINE_DELETION_REFRESH_INTERVAL_MS = 3000

export const getEngineRefreshInterval = (engines) => (
  engines.some(engine => engine.lifecycle_state === 'deleting')
    ? ENGINE_DELETION_REFRESH_INTERVAL_MS
    : ENGINE_STATUS_REFRESH_INTERVAL_MS
)
