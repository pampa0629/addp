// One owner for cancellation and late-response exclusion. Request callbacks only
// receive transport options; callers retain their own domain/UI state.
export function createLatestRequestScope() {
  let epoch = 0, disposed = false
  const pending = new Set()
  function invalidate() {
    epoch++
    for (const controller of pending) controller.abort()
    pending.clear()
    return epoch
  }
  return {
    invalidate,
    current: ticket => !disposed && ticket === epoch,
    async request(work) {
      if (disposed) throw new Error('request_scope_disposed')
      const controller = new AbortController()
      pending.add(controller)
      try { return await work({ signal: controller.signal }) }
      finally { pending.delete(controller) }
    },
    dispose() { disposed = true; invalidate() }
  }
}
