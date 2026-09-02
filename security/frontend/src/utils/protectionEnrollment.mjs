export function normalizeDiscoverySummary(enrollment) {
  const summary = enrollment?.discovery_summary
  const status = summary?.status === 'completed' ? 'completed' : 'not_completed'
  const parsedCount = Number(summary?.finding_count)
  const parsedPendingCount = Number(summary?.pending_review_count)
  const parsedReviewedCount = Number(summary?.reviewed_count)
  const findingCount = status === 'completed' && Number.isFinite(parsedCount) && parsedCount > 0
    ? Math.floor(parsedCount)
    : 0
  const reviewedCount = status === 'completed' && Number.isFinite(parsedReviewedCount) && parsedReviewedCount > 0
    ? Math.min(findingCount, Math.floor(parsedReviewedCount))
    : 0
  const pendingReviewCount = status === 'completed' && Number.isFinite(parsedPendingCount) && parsedPendingCount >= 0
    ? Math.min(findingCount, Math.floor(parsedPendingCount))
    : Math.max(0, findingCount - reviewedCount)
  return {
    status,
    findingCount,
    pendingReviewCount,
    reviewedCount
  }
}

export function isZeroFindingDiscovery(enrollment) {
  const summary = normalizeDiscoverySummary(enrollment)
  return summary.status === 'completed' && summary.findingCount === 0
}

export function buildFindingReviewPayload({ decision, sensitiveDataTypeID, securityGradeID, rationale }) {
  const normalizedDecision = String(decision || '').trim()
  const normalizedRationale = String(rationale || '').trim()
  const payload = { decision: normalizedDecision, rationale: normalizedRationale }
  if (normalizedDecision === 'adjust') {
    payload.sensitive_data_type_id = Number(sensitiveDataTypeID)
    payload.security_grade_id = Number(securityGradeID)
  }
  return payload
}

export function findingReviewState(finding) {
  const decision = String(finding?.review?.decision || '').trim()
  return ['confirm', 'adjust', 'reject'].includes(decision) ? decision : 'pending'
}

const TRANSITIONAL_ENROLLMENT_STATES = new Set(['activating', 'releasing'])

export function needsEnrollmentRefresh(enrollment) {
  if (!enrollment || enrollment.state === 'released') return false
  if (TRANSITIONAL_ENROLLMENT_STATES.has(String(enrollment.state || ''))) return true
  if (Array.isArray(enrollment.owner_progress) && enrollment.owner_progress.some(owner => !owner?.acknowledged)) {
    return true
  }
  return enrollment.state === 'enrolling' && normalizeDiscoverySummary(enrollment).status !== 'completed'
}

export function discoveryRefreshMarker(enrollment) {
  return [
    String(enrollment?.last_discovered_at || ''),
    String(enrollment?.latest_source_snapshot_hash || '')
  ].join('|')
}
