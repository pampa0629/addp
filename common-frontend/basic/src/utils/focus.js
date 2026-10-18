const DEFAULT_FOCUSABLE_SELECTOR = [
  'input:not([disabled])',
  'textarea:not([disabled])',
  'select:not([disabled])',
  'button:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

export function focusElement(target, { preventScroll = true, preserveDialogFocus = false } = {}) {
  if (!target) return false

  const element = target.$el || target
  if (preserveDialogFocus) {
    const dialog = element.closest?.('[role="dialog"], [role="alertdialog"]')
    const active = element.ownerDocument?.activeElement
    const interactive = active?.matches?.(`${DEFAULT_FOCUSABLE_SELECTOR},a[href],[contenteditable="true"]`)
    if (dialog && interactive && active !== dialog && dialog.contains(active)) return false
  }

  if (typeof target.focus === 'function') {
    target.focus({ preventScroll })
    return true
  }

  if (typeof element.focus === 'function') {
    element.focus({ preventScroll })
    return true
  }

  const focusable = element.querySelector?.(DEFAULT_FOCUSABLE_SELECTOR)
  if (!focusable) return false
  focusable.focus({ preventScroll })
  return true
}
