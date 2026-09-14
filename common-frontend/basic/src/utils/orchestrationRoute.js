// Canonical cross-module task reference filter owned by Orchestrator.
export function resolveOrchestrationTaskFilter(query = {}) {
  const values = ['module', 'task_type', 'task_id'].map(key => query[key])
  if (values.every(value => value === undefined)) return null
  const [module, taskType, taskID] = values
  if (typeof module !== 'string' || !/^[a-z][a-z0-9_]*$/.test(module) ||
      typeof taskType !== 'string' || !/^[a-z][a-z0-9_]*$/.test(taskType) ||
      !/^[1-9]\d*$/.test(String(taskID)) || Array.isArray(taskID) ||
      !Number.isSafeInteger(Number(taskID))) {
    throw new TypeError('Invalid orchestration task reference')
  }
  return { module, task_type: taskType, task_id: String(taskID) }
}

export function buildOrchestrationListRoute(task = {}) {
  const filter = resolveOrchestrationTaskFilter(task)
  return '/orchestrator/orchestrations' + (filter ? `?${new URLSearchParams(filter)}` : '')
}

export function matchesOrchestrationTask(orchestration, task) {
  const filter = resolveOrchestrationTaskFilter(task)
  if (!filter) return false
  return orchestration.steps?.some(step => step.provider === filter.module &&
    step.task_type === filter.task_type && String(step.task_id) === filter.task_id) || false
}
