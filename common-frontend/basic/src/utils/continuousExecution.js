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
