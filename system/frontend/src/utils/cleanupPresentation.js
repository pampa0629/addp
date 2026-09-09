export function formatCleanupStateChanges(summary, translate) {
  const parts = []
  const changes = [
    ['marked_missing_source', 'missing'],
    ['marked_outdated', 'outdated'],
    ['disabled_task_definitions', 'disabled'],
    ['deleted_task_definitions', 'deleted']
  ]

  for (const [field, translationKey] of changes) {
    const count = Number(summary?.[field] || 0)
    if (count > 0) {
      parts.push(translate(`system.cleanup.modules.stateChanges.${translationKey}`, { count }))
    }
  }
  return parts.length > 0 ? parts.join(' / ') : '-'
}

const cleanupResultStatuses = new Set(['completed', 'completed_with_errors'])

export function selectCleanupHistoryResults(tasks) {
  const orderedTasks = Array.isArray(tasks) ? tasks : []
  const latestScan = orderedTasks.find(task => task?.action === 'scan')
  const latestExecute = orderedTasks.find(task => task?.action === 'execute')
  const latestCompletedScan = orderedTasks.find(task => (
    task?.action === 'scan' && cleanupResultStatuses.has(task?.status)
  ))
  const latestCompletedResult = orderedTasks.find(task => cleanupResultStatuses.has(task?.status))

  return {
    latestScanTaskId: latestScan?.task_id || '',
    latestExecuteTaskId: latestExecute?.task_id || '',
    latestCompletedScanTaskId: latestCompletedScan?.task_id || '',
    latestCompletedResultTaskId: latestCompletedResult?.task_id || ''
  }
}
