export function executionDetailLocation(execution) {
  const executionID = execution?.execution_id
  if (!executionID) {
    return null
  }

  return {
    path: '/executions',
    query: { execution_id: executionID }
  }
}
