export const STALE_SOURCE_FACTS_CHANGED_REASON = 'quick view optimization source facts changed'

export function quickViewOptimizationResultAction(result = {}) {
  if (result?.status !== 'stale') {
    return {
      visible: false,
      canRerun: false,
      labelKey: ''
    }
  }
  const sourceFactsChanged = String(result?.error_message || '').trim() === STALE_SOURCE_FACTS_CHANGED_REASON
  const canRerun = !!result?.task_id && !sourceFactsChanged
  return {
    visible: true,
    canRerun,
    labelKey: canRerun
      ? 'manager.quickViewOptimization.refreshOptimization'
      : 'manager.quickViewOptimization.recreateOptimizationTask'
  }
}
