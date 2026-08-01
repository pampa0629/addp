function queriesEqual(left, right) {
  const rightKeys = Object.keys(right)
  return Object.keys(left).length === rightKeys.length &&
    rightKeys.every(key => String(left[key] || '') === String(right[key] || ''))
}

export function resolveMetadataScanRouteState(engines, tasks, routeQuery = {}) {
  const taskId = String(routeQuery.task_id || '').trim()
  const task = taskId ? tasks.find(item => String(item.id) === taskId) || null : null
  const queryEngineId = String(routeQuery.engine_id || '').trim()

  if (taskId && !task) {
    return {
      kind: 'task-unavailable',
      taskId,
      engine: engines.find(item => String(item.id) === queryEngineId) || null
    }
  }

  const engineId = task ? String(task.engine_id) : queryEngineId
  if (!engineId) {
    const firstEngine = engines[0]
    return firstEngine
      ? { kind: 'redirect', query: { engine_id: String(firstEngine.id) } }
      : { kind: 'empty' }
  }

  const engine = engines.find(item => String(item.id) === engineId) || null
  if (!engine) {
    return {
      kind: 'engine-unavailable',
      engineId,
      task,
      taskId
    }
  }

  const query = task
    ? { engine_id: engineId, task_id: taskId }
    : { engine_id: engineId }
  return {
    kind: 'ready',
    engine,
    task,
    taskId,
    query,
    changed: !queriesEqual(routeQuery, query)
  }
}
