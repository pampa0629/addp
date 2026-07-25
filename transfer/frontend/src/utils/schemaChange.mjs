export function buildSchemaChangeApproval(fields) {
  if (!Array.isArray(fields) || fields.length === 0) return null
  const normalized = fields.map((field) => ({
    source: String(field?.source || '').trim(),
    target: String(field?.target || '').trim(),
    target_type: String(field?.target_type || '').trim(),
    nullable: field?.nullable === true
  }))
  if (normalized.some((field) => !field.source || !field.target || !field.target_type || !field.nullable)) {
    return null
  }
  if (new Set(normalized.map((field) => field.source)).size !== normalized.length) return null
  if (new Set(normalized.map((field) => field.target)).size !== normalized.length) return null
  return { fields: normalized }
}

export function getSchemaChangeScanNotice(request, now = Date.now()) {
  if (request?.status !== 'applied') return null
  const status = String(request?.metadata_scan_status || '')
  const attempt = Number(request?.metadata_scan_attempt || 0)
  if (status === 'pending') return { state: 'pending', attempt, retryable: true }
  if (status === 'failed') return { state: 'failed', attempt, retryable: false }
  if (status !== 'running') return null
  const leaseUntil = Date.parse(request?.metadata_scan_lease_until || '')
  const expired = Number.isFinite(leaseUntil) && leaseUntil <= now
  return { state: expired ? 'expired' : 'running', attempt, retryable: expired }
}

export function buildSchemaChangeScanRetry(request) {
  if (!getSchemaChangeScanNotice(request)?.retryable) return null
  return buildSchemaChangeApproval(request?.approved_mappings)
}
