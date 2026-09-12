export function executionDetailRoute(executionId) {
  const normalizedID = typeof executionId === 'string' ? executionId.trim() : ''
  if (!normalizedID) return null

  return {
    name: 'ExecutionDetail',
    params: { execution_id: normalizedID }
  }
}
