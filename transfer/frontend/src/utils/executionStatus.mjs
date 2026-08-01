const STATUS_LABEL_KEYS = {
  pending: 'transfer.executionStatus.pending',
  running: 'transfer.executionStatus.running',
  success: 'transfer.executionStatus.success',
  failed: 'transfer.executionStatus.failed',
  cancelled: 'transfer.executionStatus.cancelled'
}

const STATUS_TAG_TYPES = {
  pending: 'info',
  running: 'primary',
  success: 'success',
  failed: 'danger',
  cancelled: 'info'
}

export function executionStatusLabelKey(status) {
  const normalized = String(status || 'pending').toLowerCase()
  return STATUS_LABEL_KEYS[normalized] || ''
}

export function executionStatusTagType(status) {
  return STATUS_TAG_TYPES[String(status || 'pending').toLowerCase()] || 'info'
}
