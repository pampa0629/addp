const hasValue = value => value !== null && value !== undefined && String(value).trim() !== ''

export function buildMonitorExecutionRoute(executionId) {
  if (!hasValue(executionId)) return ''
  return `/monitor/executions?execution_id=${encodeURIComponent(String(executionId))}`
}

// A result page can return only to this execution's canonical Monitor context.
export function resolveMonitorExecutionReturnRoute(executionId, returnTo) {
  if (!hasValue(executionId)) return ''
  if (returnTo === undefined) return buildMonitorExecutionRoute(executionId)
  if (typeof returnTo !== 'string' || !returnTo.startsWith('/monitor/executions?')) return ''
  const url = new URL(returnTo, 'http://console.invalid')
  const allowed = new Set(['module', 'task_type', 'source_task_id', 'source', 'status', 'trigger_type', 'page', 'page_size', 'execution_id'])
  const keys = [...url.searchParams.keys()]
  if (url.pathname !== '/monitor/executions' || url.hash || new Set(keys).size !== keys.length || keys.some(key => !allowed.has(key)) || url.searchParams.get('execution_id') !== String(executionId)) return ''
  return url.pathname + url.search
}

