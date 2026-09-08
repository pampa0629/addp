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
