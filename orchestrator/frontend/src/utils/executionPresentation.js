export const isActiveExecution = execution => ['pending', 'running'].includes(execution?.status)

export function executionStepStates(execution, steps) {
  if (!execution) return {}
  const results = execution.metadata?.step_results || {}
  const active = isActiveExecution(execution)
  return Object.fromEntries(steps.map(step => {
    const result = results[step.id]
    const status = result?.status || (execution.status === 'running' && execution.current_step === step.id
      ? 'running' : active ? 'pending' : 'not_started')
    return [step.id, {
      status,
      error: result?.error || '',
      duration: result?.duration,
      executionId: result?.result?.execution_id || ''
    }]
  }))
}

export function executionStatusType(status) {
  return ({ running: 'warning', success: 'success', failed: 'danger', timeout: 'danger' })[status] || 'info'
}
