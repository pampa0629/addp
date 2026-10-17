// Already delivered values cannot be recalled. This controller clears the
// visible result at its server deadline and rechecks revocation while open.
export function createQueryPlaintextRefresh({
  fetchExecution, normalize, onResult, onClear,
  now = Date.now, setTimer = setTimeout, clearTimer = clearTimeout
}) {
  let timer = null
  let sequence = 0
  let disposed = false

  function stop() {
    sequence++
    if (timer !== null) clearTimer(timer)
    timer = null
  }

  function schedule(result) {
    stop()
    const until = Date.parse(result?.plaintext_expires_at || '')
    const id = result?.execution_id
    if (disposed || !id || !Number.isFinite(until)) return
    const request = sequence
    timer = setTimer(async () => {
      timer = null
      if (disposed || request !== sequence) return
      if (now() >= until) onClear()
      try {
        const execution = await fetchExecution(id)
        if (disposed || request !== sequence) return
        const next = normalize(execution)
        onResult(next)
        schedule(next)
      } catch {
        if (!disposed && request === sequence) {
          stop()
          onClear()
        }
      }
    }, Math.max(0, Math.min(5000, until - now())))
  }

  return {
    schedule,
    stop,
    dispose() {
      disposed = true
      stop()
    }
  }
}
