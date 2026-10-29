export function hasActiveBuild(task) {
  return Boolean(task?.execution_id) && ['pending', 'running'].includes(task.status)
}

export function canCancelBuild(task) {
  return hasActiveBuild(task) && Number.isInteger(task.execution_attempt) && task.execution_attempt >= 0
}

export function canRunBuild(task) {
  return Boolean(task) && !hasActiveBuild(task) && ['pending', 'failed', 'timeout', 'cancelled'].includes(task.status)
}

export function buildCancellationIdentity(task) {
  if (!canCancelBuild(task)) throw new Error('Current execution identity is required')
  return { execution_id: task.execution_id, attempt: task.execution_attempt }
}
