import { describe, expect, it } from 'vitest'
import {
  buildFindingReviewPayload,
  discoveryRefreshMarker,
  findingReviewState,
  isZeroFindingDiscovery,
  needsEnrollmentRefresh,
  normalizeDiscoverySummary,
  resolvePendingReviewContinuation,
  resolveReviewQueueFilters
} from '../src/utils/protectionEnrollment.mjs'

describe('protection enrollment discovery summary', () => {
  it('does not treat a missing or incomplete discovery as zero findings', () => {
    expect(normalizeDiscoverySummary({})).toEqual({ status: 'not_completed', findingCount: 0, pendingReviewCount: 0, reviewedCount: 0 })
    expect(isZeroFindingDiscovery({ discovery_summary: { status: 'not_completed', finding_count: 0 } })).toBe(false)
  })

  it('only exposes the no-protection confirmation for a completed zero-finding snapshot', () => {
    expect(isZeroFindingDiscovery({ discovery_summary: { status: 'completed', finding_count: 0 } })).toBe(true)
    expect(isZeroFindingDiscovery({ discovery_summary: { status: 'completed', finding_count: 1 } })).toBe(false)
  })

  it('normalizes invalid finding counts without inventing findings', () => {
    expect(normalizeDiscoverySummary({ discovery_summary: { status: 'completed', finding_count: -2 } })).toEqual({ status: 'completed', findingCount: 0, pendingReviewCount: 0, reviewedCount: 0 })
  })

  it('keeps current-snapshot review counts within the finding total', () => {
    expect(normalizeDiscoverySummary({ discovery_summary: { status: 'completed', finding_count: 3, pending_review_count: 1, reviewed_count: 2 } })).toEqual({
      status: 'completed', findingCount: 3, pendingReviewCount: 1, reviewedCount: 2
    })
  })
})

describe('protection enrollment refresh decisions', () => {
  it('refreshes only while enrollment or owner synchronization is transitional', () => {
    expect(needsEnrollmentRefresh({ state: 'enrolling', discovery_summary: { status: 'not_completed' }, owner_progress: [] })).toBe(true)
    expect(needsEnrollmentRefresh({ state: 'enrolling', discovery_summary: { status: 'completed', finding_count: 0 }, owner_progress: [{ acknowledged: true }] })).toBe(false)
    expect(needsEnrollmentRefresh({ state: 'active', owner_progress: [{ acknowledged: false }] })).toBe(true)
    expect(needsEnrollmentRefresh({ state: 'active', owner_progress: [{ acknowledged: true }] })).toBe(false)
    expect(needsEnrollmentRefresh({ state: 'released', owner_progress: [{ acknowledged: false }] })).toBe(false)
  })

  it('tracks both discovery completion time and source snapshot changes', () => {
    expect(discoveryRefreshMarker({ last_discovered_at: '2026-09-02T10:00:00Z', latest_source_snapshot_hash: 'a' }))
      .toBe('2026-09-02T10:00:00Z|a')
    expect(discoveryRefreshMarker({})).toBe('|')
  })
})

describe('finding review form', () => {
  it('only sends adjusted type and grade for an adjust decision', () => {
    expect(buildFindingReviewPayload({ decision: 'confirm', sensitiveDataTypeID: '4', securityGradeID: '5', rationale: '  confirmed  ' })).toEqual({ decision: 'confirm', rationale: 'confirmed' })
    expect(buildFindingReviewPayload({ decision: 'adjust', sensitiveDataTypeID: '4', securityGradeID: '5', rationale: ' updated ' })).toEqual({ decision: 'adjust', sensitive_data_type_id: 4, security_grade_id: 5, rationale: 'updated' })
  })

  it('derives the immutable first-review state', () => {
    expect(findingReviewState({})).toBe('pending')
    expect(findingReviewState({ review: { decision: 'reject' } })).toBe('reject')
  })
})

describe('review queue recoverable filters', () => {
  it('keeps only canonical non-default filter and pagination values', () => {
    expect(resolveReviewQueueFilters({ sensitive_data_type_id: '9', detector_version: ' addp.detector.phone_metadata/v2 ', page: '3', page_size: '50' })).toEqual({
      sensitiveDataTypeID: '9', detectorVersion: 'addp.detector.phone_metadata/v2', page: 3, pageSize: 50,
      query: { sensitive_data_type_id: '9', detector_version: 'addp.detector.phone_metadata/v2', page: '3', page_size: '50' }
    })
  })

  it('drops invalid values and omits defaults from the canonical query', () => {
    expect(resolveReviewQueueFilters({ sensitive_data_type_id: '-1', detector_version: 'x'.repeat(101), page: '1', page_size: '7' })).toEqual({
      sensitiveDataTypeID: '', detectorVersion: '', page: 1, pageSize: 20, query: {}
    })
  })
})

describe('pending review continuation', () => {
  it('opens the candidate that moved into the reviewed row position', () => {
    const next = resolvePendingReviewContinuation({ rows: [{ id: 'b' }, { id: 'c' }], total: 2, page: 1, pageSize: 20, reviewedIndex: 1 })
    expect(next).toEqual({ page: 1, finding: { id: 'c' }, reload: false })
  })

  it('moves to the new last page when reviewing empties the old last page', () => {
    const next = resolvePendingReviewContinuation({ rows: [], total: 40, page: 3, pageSize: 20, reviewedIndex: 0 })
    expect(next).toEqual({ page: 2, finding: null, reload: true })
  })

  it('finishes cleanly when no pending candidates remain', () => {
    const next = resolvePendingReviewContinuation({ rows: [], total: 0, page: 1, pageSize: 20, reviewedIndex: 0 })
    expect(next).toEqual({ page: 1, finding: null, reload: false })
  })
})
