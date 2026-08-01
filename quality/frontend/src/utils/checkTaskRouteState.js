function queryValue(value) {
  const firstValue = Array.isArray(value) ? value[0] : value
  return firstValue == null ? '' : String(firstValue).trim()
}

function positiveInteger(value) {
  const normalized = queryValue(value)
  if (!/^\d+$/.test(normalized)) return ''
  const number = Number(normalized)
  return Number.isSafeInteger(number) && number > 0 ? String(number) : ''
}

function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length && rightKeys.every(key => (
    !Array.isArray(left[key]) && queryValue(left[key]) === queryValue(right[key])
  ))
}

export function resolveCheckTaskRouteState(routeQuery = {}) {
  const taskID = positiveInteger(routeQuery.task_id)
  const create = queryValue(routeQuery.create) === '1'
  const query = taskID
    ? { task_id: taskID }
    : create
      ? { create: '1' }
      : {}

  return {
    mode: taskID ? 'edit' : create ? 'create' : 'list',
    taskID,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
