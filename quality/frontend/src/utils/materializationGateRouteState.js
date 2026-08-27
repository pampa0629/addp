const DEFAULT_PAGE_SIZE = 20
const ALLOWED_PAGE_SIZES = new Set([20, 50, 100])

const queryValue = value => {
  const first = Array.isArray(value) ? value[0] : value
  return first == null ? '' : String(first).trim()
}

const positiveInteger = (value, fallback) => {
  const normalized = queryValue(value)
  if (!/^\d+$/.test(normalized)) return fallback
  const number = Number(normalized)
  return Number.isSafeInteger(number) && number > 0 ? number : fallback
}

const queriesEqual = (left, right) => {
  const keys = Object.keys(right)
  return Object.keys(left).length === keys.length && keys.every(key => !Array.isArray(left[key]) && String(left[key]) === right[key])
}

export const buildMaterializationGateRouteQuery = ({ mode, taskID, page, pageSize }) => {
  const query = {}
  if (mode === 'edit' && taskID) query.task_id = String(taskID)
  else if (mode === 'create') query.create = '1'
  if (page > 1) query.page = String(page)
  if (pageSize !== DEFAULT_PAGE_SIZE) query.page_size = String(pageSize)
  return query
}

export const resolveMaterializationGateRouteState = (routeQuery = {}) => {
  const taskIDValue = positiveInteger(routeQuery.task_id, null)
  const taskID = taskIDValue ? String(taskIDValue) : ''
  const mode = taskID ? 'edit' : queryValue(routeQuery.create) === '1' ? 'create' : 'list'
  const page = positiveInteger(routeQuery.page, 1)
  const requestedPageSize = positiveInteger(routeQuery.page_size, DEFAULT_PAGE_SIZE)
  const pageSize = ALLOWED_PAGE_SIZES.has(requestedPageSize) ? requestedPageSize : DEFAULT_PAGE_SIZE
  const query = buildMaterializationGateRouteQuery({ mode, taskID, page, pageSize })
  return { mode, taskID, page, pageSize, query, changed: !queriesEqual(routeQuery, query) }
}
