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

export function resolveReviewQueueFilters(routeQuery = {}) {
  const queryValue = value => String(Array.isArray(value) ? value[0] || '' : value || '').trim()
  const parsedTypeID = Number(queryValue(routeQuery.sensitive_data_type_id))
  const sensitiveDataTypeID = Number.isSafeInteger(parsedTypeID) && parsedTypeID > 0 ? String(parsedTypeID) : ''
  const detectorVersionValue = queryValue(routeQuery.detector_version)
  const detectorVersion = detectorVersionValue.length <= 100 ? detectorVersionValue : ''
  const parsedPage = Number(queryValue(routeQuery.page))
  const page = Number.isSafeInteger(parsedPage) && parsedPage > 1 ? parsedPage : 1
  const parsedPageSize = Number(queryValue(routeQuery.page_size))
  const pageSize = [20, 50, 100].includes(parsedPageSize) ? parsedPageSize : 20
  const query = {}
  if (sensitiveDataTypeID) query.sensitive_data_type_id = sensitiveDataTypeID
  if (detectorVersion) query.detector_version = detectorVersion
  if (page > 1) query.page = String(page)
  if (pageSize !== 20) query.page_size = String(pageSize)
  return { sensitiveDataTypeID, detectorVersion, page, pageSize, query }
}

export function resolvePendingReviewContinuation({ rows, total, page, pageSize, reviewedIndex }) {
  const candidates = Array.isArray(rows) ? rows : []
  const normalizedTotal = Number.isSafeInteger(Number(total)) && Number(total) > 0 ? Number(total) : 0
  const normalizedPageSize = Number.isSafeInteger(Number(pageSize)) && Number(pageSize) > 0 ? Number(pageSize) : 20
  const normalizedPage = Number.isSafeInteger(Number(page)) && Number(page) > 0 ? Number(page) : 1
  const lastPage = Math.max(1, Math.ceil(normalizedTotal / normalizedPageSize))
  const targetPage = Math.min(normalizedPage, lastPage)
  if (targetPage !== normalizedPage) return { page: targetPage, finding: null, reload: true }
  if (candidates.length === 0) return { page: targetPage, finding: null, reload: false }
  const parsedIndex = Number(reviewedIndex)
  const targetIndex = Number.isSafeInteger(parsedIndex) && parsedIndex >= 0
    ? Math.min(parsedIndex, candidates.length - 1)
    : 0
  return { page: targetPage, finding: candidates[targetIndex], reload: false }
}

export function findingReviewState(finding) {
  const decision = String(finding?.review?.decision || '').trim()
  return ['confirm', 'adjust', 'reject'].includes(decision) ? decision : 'pending'
}

const FINDING_DECISION_STATES = new Set([
  'automatic',
  'formal',
  'awaiting_review',
  'detector_inactive',
  'baseline_missing',
  'rejected',
  'revoked',
  'superseded'
])

export function findingDecisionState(finding) {
  const state = String(finding?.explanation?.decision_state || '').trim()
  return FINDING_DECISION_STATES.has(state) ? state : 'awaiting_review'
}

export function findingOutletRules(finding, consumerOwner) {
  const outlets = Array.isArray(finding?.explanation?.outlets) ? finding.explanation.outlets : []
  const outlet = outlets.find(item => String(item?.consumer_owner || '') === String(consumerOwner || ''))
  if (!outlet) return null
  return {
    consumerOwner: String(outlet.consumer_owner || ''),
    projectionState: String(outlet.projection_state || ''),
    acknowledged: Boolean(outlet.acknowledged),
    rules: Array.isArray(outlet.rules)
      ? outlet.rules.map(rule => ({
        action: String(rule?.action || ''),
        effect: String(rule?.effect || ''),
        algorithm: String(rule?.algorithm || '')
      })).filter(rule => rule.action && rule.effect)
      : []
  }
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
