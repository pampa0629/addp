const DEV_TYPES = new Set(['query', 'workflow', 'script'])
const EXECUTION_STATUSES = new Set(['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'])
const TRIGGER_TYPES = new Set(['manual', 'scheduled'])
const PAGE_SIZES = new Set([10, 20, 50, 100])

function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

function positiveInteger(value) {
  const normalized = queryValue(value)
  if (!/^\d+$/.test(normalized)) return 0
  const number = Number(normalized)
  return Number.isSafeInteger(number) && number > 0 ? number : 0
}

function validDate(value) {
  const normalized = queryValue(value)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(normalized)) return ''
  const date = new Date(`${normalized}T00:00:00Z`)
  return Number.isNaN(date.getTime()) || date.toISOString().slice(0, 10) !== normalized ? '' : normalized
}

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length && rightKeys.every(key => (
    !Array.isArray(left[key]) && queryValue(left[key]) === queryValue(right[key])
  ))
}

export function resolveExecutionMonitorRouteState(routeQuery = {}) {
  const devType = queryValue(routeQuery.dev_type)
  const status = queryValue(routeQuery.status)
  const triggerType = queryValue(routeQuery.trigger_type)
  const sourceTaskID = positiveInteger(routeQuery.source_task_id)
  const startDate = validDate(routeQuery.start_date)
  const endDate = validDate(routeQuery.end_date)
  const page = positiveInteger(routeQuery.page) || 1
  const requestedPageSize = positiveInteger(routeQuery.page_size)
  const pageSize = PAGE_SIZES.has(requestedPageSize) ? requestedPageSize : 20
  const query = {}

  if (DEV_TYPES.has(devType)) query.dev_type = devType
  if (EXECUTION_STATUSES.has(status)) query.status = status
  if (TRIGGER_TYPES.has(triggerType)) query.trigger_type = triggerType
  if (sourceTaskID) query.source_task_id = String(sourceTaskID)
  if (startDate && endDate && startDate <= endDate) {
    query.start_date = startDate
    query.end_date = endDate
  }
  if (page > 1) query.page = String(page)
  if (pageSize !== 20) query.page_size = String(pageSize)

  return {
    filters: {
      dev_type: DEV_TYPES.has(devType) ? devType : '',
      status: EXECUTION_STATUSES.has(status) ? status : '',
      trigger_type: TRIGGER_TYPES.has(triggerType) ? triggerType : '',
      source_task_id: sourceTaskID ? String(sourceTaskID) : ''
    },
    dateRange: query.start_date ? [query.start_date, query.end_date] : [],
    page,
    pageSize,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
