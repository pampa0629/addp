// Execution history carries stable codes; user messages follow the current UI
// language instead of displaying stored worker text.
export const profileFailureMessage = (execution, translate) => {
  if (!['failed', 'timeout'].includes(execution?.status)) return ''
  return translate(execution.error_code === 'protection_version_changed'
    ? 'manager.explorer.profile.protectionVersionChanged'
    : 'manager.explorer.profile.latestFailed')
}
