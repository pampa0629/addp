import { resolveIssueListRouteState } from './issueListRouteState.js'

function positiveInteger(value) {
  const normalized = value == null ? '' : String(value).trim()
  if (!/^\d+$/.test(normalized)) return ''

  const number = Number(normalized)
  return Number.isSafeInteger(number) && number > 0 ? String(number) : ''
}

export function issueDetailRoute(issueId, listQuery = {}) {
  const normalizedID = positiveInteger(issueId)
  if (!normalizedID) return null

  return {
    name: 'IssueDetail',
    params: { id: normalizedID },
    query: resolveIssueListRouteState(listQuery).query
  }
}

export function issueExecutionRoute(executionId) {
  const normalizedID = typeof executionId === 'string' ? executionId.trim() : ''
  if (!normalizedID) return null

  return {
    name: 'ExecutionDetail',
    params: { execution_id: normalizedID }
  }
}
