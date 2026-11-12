// Already delivered values cannot be recalled. This controller clears the
// visible result at its server deadline and rechecks revocation while open.
export function createQueryPlaintextRefresh({
  fetchExecution, normalize, onResult, onClear,
  now = Date.now, setTimer = setTimeout, clearTimer = clearTimeout
}) {
  let timer = null
  let deadlineTimer = null
  let sequence = 0
  let disposed = false

  function stop() {
    sequence++
    if (timer !== null) clearTimer(timer)
    timer = null
    if (deadlineTimer !== null) clearTimer(deadlineTimer)
    deadlineTimer = null
  }

  function schedule(result) {
    stop()
    const until = Date.parse(result?.plaintext_expires_at || '')
    const id = result?.execution_id
    if (disposed || !id || !Number.isFinite(until)) return
    const request = sequence
    let expired = false
    const expire = () => {
      if (disposed || request !== sequence || expired) return
      expired = true
      onClear()
    }
    if (now() >= until) expire()
    else deadlineTimer = setTimer(() => { deadlineTimer = null; expire() }, until - now())
    timer = setTimer(async () => {
      timer = null
      if (disposed || request !== sequence) return
      if (now() >= until) expire()
      try {
        const execution = await fetchExecution(id)
        if (disposed || request !== sequence) return
        const next = normalize(execution)
        const nextUntil = Date.parse(next?.plaintext_expires_at || '')
        if (Number.isFinite(nextUntil) && now() >= nextUntil) {
          if (!expired) onClear()
          stop()
          return
        }
        onResult(next)
        schedule(next)
      } catch {
        if (!disposed && request === sequence) {
          stop()
          if (!expired) onClear()
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
