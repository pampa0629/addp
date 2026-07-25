function objectValue(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

function numberValue(value) {
  if (value === null || value === undefined || value === '') return null
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

export function getContinuousDiagnostics(metadata) {
  return objectValue(objectValue(metadata).continuous?.diagnostics)
}

export function hasContinuousExecutionMetadata(metadata) {
  const value = objectValue(metadata)
  if (Object.keys(objectValue(value.continuous)).length > 0) return true
  return [
    'recovery_reason',
    'recovered_from_execution_id',
    'recovery_attempt',
    'recovery_consecutive_failures',
    'recovery_not_before',
    'recovery_circuit_state'
  ].some(key => value[key] !== undefined && value[key] !== null && value[key] !== '')
}

export function buildContinuousPartitionRows(metadata) {
  const continuous = objectValue(objectValue(metadata).continuous)
  const committed = objectValue(continuous.partitions)
  const diagnostics = getContinuousDiagnostics(metadata)
  const diagnosticPartitions = objectValue(diagnostics.partitions)
  const partitionNames = new Set([...Object.keys(committed), ...Object.keys(diagnosticPartitions)])

  return Array.from(partitionNames, partition => {
    const position = objectValue(committed[partition])
    const values = objectValue(position.values)
    const diagnostic = objectValue(diagnosticPartitions[partition])
    return {
      partition,
      nextOffset: numberValue(diagnostic.next_offset ?? values.next_offset),
      earliestOffset: numberValue(diagnostic.earliest_offset),
      latestOffset: numberValue(diagnostic.latest_offset),
      lagRecords: numberValue(diagnostic.lag_records),
      recoveryHeadroomRecords: numberValue(diagnostic.recovery_headroom_records),
      sourceRateRecordsPerSecond: numberValue(diagnostic.source_rate_records_per_second),
      retentionHorizonSeconds: numberValue(diagnostic.retention_horizon_seconds),
      health: diagnostic.health || 'unknown',
      checkpointAgeSeconds: numberValue(diagnostic.checkpoint_age_seconds),
      checkpointHealth: diagnostic.checkpoint_health || 'unknown',
      positionType: [position.type, position.version].filter(Boolean).join('/') || '-'
    }
  }).sort((left, right) => String(left.partition).localeCompare(String(right.partition), undefined, { numeric: true }))
}

export function continuousHealthTagType(health) {
  return {
    healthy: 'success',
    degraded: 'warning',
    critical: 'danger',
    unknown: 'info'
  }[health] || 'info'
}

export function formatContinuousRate(value) {
  const rate = numberValue(value)
  if (rate === null) return '-'
  if (rate >= 100) return rate.toFixed(0)
  if (rate >= 1) return rate.toFixed(1)
  return rate.toFixed(2)
}

export function formatContinuousDurationSeconds(value) {
  const seconds = numberValue(value)
  if (seconds === null || seconds < 0) return '-'
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86400) return `${(seconds / 3600).toFixed(seconds < 36000 ? 1 : 0)}h`
  return `${(seconds / 86400).toFixed(seconds < 864000 ? 1 : 0)}d`
}

export function getContinuousRecovery(metadata, status, now = Date.now()) {
  const value = objectValue(metadata)
  const hasRecovery = hasContinuousExecutionMetadata(value) && [
    'recovery_reason', 'recovered_from_execution_id', 'recovery_attempt',
    'recovery_consecutive_failures', 'recovery_not_before', 'recovery_circuit_state'
  ].some(key => value[key] !== undefined && value[key] !== null && value[key] !== '')
  if (!hasRecovery) return null

  const notBefore = value.recovery_not_before || null
  const notBeforeMs = notBefore ? new Date(notBefore).getTime() : Number.NaN
  const waiting = status === 'pending' && Number.isFinite(notBeforeMs) && notBeforeMs > now
  const circuitState = ['closed', 'open', 'half_open'].includes(value.recovery_circuit_state)
    ? value.recovery_circuit_state
    : 'closed'

  const active = status === 'pending' || status === 'running'
  let state = 'completed'
  if (active && circuitState === 'open') state = 'open'
  else if (active && circuitState === 'half_open') state = 'half_open'
  else if (waiting) state = 'waiting'
  else if (status === 'pending') state = 'ready'
  else if (status === 'running') state = 'running'

  return {
    state,
    reason: value.recovery_reason || '',
    attempt: numberValue(value.recovery_attempt),
    consecutiveFailures: numberValue(value.recovery_consecutive_failures) ?? 0,
    backoffSeconds: numberValue(value.recovery_backoff_seconds) ?? 0,
    circuitState,
    notBefore,
    recoveredFromExecutionId: value.recovered_from_execution_id || '',
    waitMilliseconds: waiting ? Math.max(0, notBeforeMs - now) : 0
  }
}

export function continuousRecoveryTagType(state) {
  return ({
    open: 'danger',
    half_open: 'warning',
    waiting: 'warning',
    ready: 'info',
    running: 'primary',
    completed: 'success'
  })[state] || 'info'
}

export function formatRecoverySeconds(value) {
  const seconds = numberValue(value)
  if (seconds === null) return '-'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Number((seconds / 60).toFixed(1))}m`
  return `${Number((seconds / 3600).toFixed(1))}h`
}

export function buildContinuousSignals(metadata, status, now = Date.now()) {
  const value = objectValue(metadata)
  const continuous = objectValue(value.continuous)
  const diagnostics = getContinuousDiagnostics(value)
  const recovery = getContinuousRecovery(metadata, status, now)
  const schemaChange = objectValue(continuous.schema_change)
  const signals = []

  if (recovery?.state === 'open') {
    signals.push({ code: 'recovery_circuit_open', severity: 'critical', recovery })
  } else if (recovery?.state === 'half_open') {
    signals.push({ code: 'recovery_half_open', severity: 'warning', recovery })
  } else if (recovery?.state === 'waiting') {
    signals.push({ code: 'recovery_waiting', severity: 'warning', recovery })
  } else if (recovery?.state === 'ready') {
    signals.push({ code: 'recovery_ready', severity: 'warning', recovery })
  }

  if (diagnostics.health === 'critical') {
    signals.push({ code: 'retention_critical', severity: 'critical', diagnostics })
  } else if (diagnostics.health === 'degraded') {
    signals.push({ code: 'retention_degraded', severity: 'warning', diagnostics })
  }

  if (diagnostics.checkpoint_health === 'degraded') {
    signals.push({ code: 'checkpoint_stalled', severity: 'warning', diagnostics })
  }
  if (diagnostics.error) {
    signals.push({ code: 'diagnostics_error', severity: 'warning', diagnostics })
  }
  if (schemaChange.status === 'pending') {
    signals.push({ code: 'schema_change_blocked', severity: 'critical', schemaChange })
  }

  const rank = { critical: 0, warning: 1, info: 2 }
  return signals.sort((left, right) => rank[left.severity] - rank[right.severity])
}

export function continuousSignalTagType(severity) {
  return ({ critical: 'danger', warning: 'warning', info: 'info' })[severity] || 'info'
}
