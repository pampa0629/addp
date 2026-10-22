export const runtimeInstanceStopReasons = ['graceful', 'lease_expired']

export function normalizeRuntimeInstanceStopReason(value) {
  const reason = typeof value === 'string' ? value.trim() : ''
  return runtimeInstanceStopReasons.includes(reason) ? reason : ''
}

export function runtimeInstanceStopReasonLabelKey(reason) {
  return runtimeInstanceStopReasons.includes(reason) ? `common.runtimeInstanceStopReason.reasons.${reason}` : ''
}
