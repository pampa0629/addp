export const autoRefreshOptions = ['15', '10', '30', '60', 'off']

// Call schedule only after the owner's complete request cycle has settled.
export function createAutoRefresh({ interval, enabled, refresh, pause, visibilityChanged, document: target = globalThis.document, timers = globalThis }) {
  let timer = null, disposed = false, visible = !target.hidden
  const ready = () => !disposed && visible && enabled() && autoRefreshOptions.includes(interval()) && interval() !== 'off'
  function stop() { timers.clearTimeout(timer); timer = null }
  function schedule() {
    stop()
    if (!ready()) return
    timer = timers.setTimeout(() => {
      timer = null
      if (ready()) void refresh()
    }, Number(interval()) * 1000)
  }
  function onVisibility() {
    visible = !target.hidden
    visibilityChanged(visible)
    stop()
    if (!visible) pause()
    else if (ready()) void refresh()
  }
  target.addEventListener('visibilitychange', onVisibility)
  return { stop, schedule, dispose() { disposed = true; stop(); target.removeEventListener('visibilitychange', onVisibility) } }
}
