/** Monitor owns query validation; these builders share its local and Console paths. */
export function buildMonitorEngineResourcesLocation(query = {}) {
  if (!query || Object.getPrototypeOf(query) !== Object.prototype || Object.values(query).some(value => typeof value !== 'string')) {
    throw new TypeError('Engine resource route query must contain string values')
  }
  return { path: '/engine-resources', query: { ...query } }
}

export function buildMonitorEngineResourcesRoute(query = {}) {
  const location = buildMonitorEngineResourcesLocation(query)
  const search = new URLSearchParams(location.query).toString()
  return `/monitor${location.path}${search ? `?${search}` : ''}`
}
