import { debounce } from './index.js'

/** Schedule automatic list filters without letting pending text race refresh or navigation. */
export function createAutoFilter({ apply, enabled, visible, pause }) {
  let pending = false, disposed = false
  const deferred = debounce(() => submit())
  function cancel() { deferred.cancel(); pending = false }
  function submit() {
    cancel()
    if (!disposed && enabled()) return apply()
  }
  function queue() {
    cancel()
    if (disposed || !enabled()) return
    pending = true
    pause()
    if (visible()) deferred()
  }
  function visibilityChanged(isVisible) {
    if (!isVisible) deferred.cancel()
    else if (pending && !disposed) deferred()
  }
  function dispose() { disposed = true; cancel() }
  return { queue, submit, cancel, visibilityChanged, dispose, get pending() { return pending } }
}
