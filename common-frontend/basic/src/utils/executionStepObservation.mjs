const terminalStatuses = new Set(['success', 'failed', 'timeout', 'cancelled'])
const unfinishedStepStatuses = new Set(['pending', 'running'])

// A finished parent leaves the last recorded step fact; it cannot prove child termination.
export function isStepObservationStopped(executionStatus, stepStatus) {
  return terminalStatuses.has(executionStatus) && unfinishedStepStatuses.has(stepStatus)
}
