import { resolveExecutionListRouteState } from './executionListRouteState.js'

export function executionDetailRoute(executionId, listQuery = {}) {
  const normalizedID = typeof executionId === 'string' ? executionId.trim() : ''
  if (!normalizedID) return null

  return {
    name: 'ExecutionDetail',
    params: { execution_id: normalizedID },
    query: resolveExecutionListRouteState(listQuery).query
  }
}
