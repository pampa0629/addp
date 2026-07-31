const DEFAULT_FOCUSABLE_SELECTOR = [
  'input:not([disabled])',
  'textarea:not([disabled])',
  'select:not([disabled])',
  'button:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

export function focusElement(target, { preventScroll = true } = {}) {
  if (!target) return false

  if (typeof target.focus === 'function') {
    target.focus({ preventScroll })
    return true
  }

  const element = target.$el || target
  if (typeof element.focus === 'function') {
    element.focus({ preventScroll })
    return true
  }

  const focusable = element.querySelector?.(DEFAULT_FOCUSABLE_SELECTOR)
  if (!focusable) return false
  focusable.focus({ preventScroll })
  return true
}
