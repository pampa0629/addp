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
