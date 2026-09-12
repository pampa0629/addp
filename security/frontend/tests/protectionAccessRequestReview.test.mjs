import { describe, expect, it, vi } from 'vitest'
import { useProtectionAccessRequestReview } from '../src/composables/useProtectionAccessRequestReview.mjs'

function createReview(overrides = {}) {
  const dependencies = {
    canReview: true,
    listRequests: vi.fn().mockResolvedValue({ data: [{ id: 1 }], total: 1, total_pages: 1 }),
    getRequest: vi.fn(),
    decideRequest: vi.fn(),
    onQueueLoadError: vi.fn(),
    ...overrides
  }
  return { dependencies, review: useProtectionAccessRequestReview(dependencies) }
}

describe('protection access request review', () => {
  it('maps filters to the canonical server query and corrects an out-of-range page', async () => {
    const listRequests = vi.fn()
      .mockResolvedValueOnce({ data: [], total: 21, total_pages: 3 })
      .mockResolvedValueOnce({ data: [{ id: 21 }], total: 21, total_pages: 3 })
    const { review } = createReview({ listRequests })
    review.scope.value = 'history'
    review.pageSize.value = 10
    review.filters.resourceSearch = ' customer '
    review.filters.requesterSearch = ' alice '
    review.filters.state = 'approved'
    review.filters.authorizationState = 'active'
    review.createdRange.value = [new Date('2026-09-01T00:00:00Z'), new Date('2026-09-12T00:00:00Z')]

    await expect(review.loadQueue(5)).resolves.toBe(true)

    expect(listRequests).toHaveBeenNthCalledWith(1, {
      scope: 'history',
      state: 'approved',
      authorization_state: 'active',
      requester_search: ' alice ',
      resource_search: ' customer ',
      created_from: '2026-09-01T00:00:00.000Z',
      created_to: '2026-09-12T00:00:00.000Z',
      page: 5,
      page_size: 10
    })
    expect(listRequests).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 3 }))
    expect(review.page.value).toBe(3)
    expect(review.rows.value).toEqual([{ id: 21 }])
    expect(review.total.value).toBe(21)
  })

  it('clears history-only filters on scope changes and resets all filters explicitly', async () => {
    const { review } = createReview()
    review.scope.value = 'pending'
    review.filters.resourceSearch = 'orders'
    review.filters.requesterSearch = 'alice'
    review.filters.state = 'approved'
    review.filters.authorizationState = 'active'
    review.createdRange.value = [new Date('2026-09-01T00:00:00Z')]

    await review.changeScope()
    expect(review.filters).toMatchObject({ resourceSearch: 'orders', requesterSearch: 'alice', state: '', authorizationState: '' })
    expect(review.page.value).toBe(1)

    await review.resetFilters()
    expect(review.filters).toEqual({ resourceSearch: '', requesterSearch: '', state: '', authorizationState: '' })
    expect(review.createdRange.value).toEqual([])
  })

  it('rejects a slower queue response after a newer query starts', async () => {
    const pending = []
    const listRequests = vi.fn(() => new Promise(resolve => pending.push(resolve)))
    const { review } = createReview({ listRequests })

    const firstLoad = review.loadQueue(1)
    const secondLoad = review.loadQueue(2)
    pending[1]({ data: [{ id: 2 }], total: 2, total_pages: 2 })
    await secondLoad
    pending[0]({ data: [{ id: 1 }], total: 2, total_pages: 2 })
    await firstLoad

    expect(review.rows.value).toEqual([{ id: 2 }])
    expect(review.loading.value).toBe(false)
  })

  it('reloads the authoritative decision baseline and detects processed requests', async () => {
    const getRequest = vi.fn()
      .mockResolvedValueOnce({ id: 1, state: 'pending', can_decide: true, version: 2 })
      .mockResolvedValueOnce({ id: 1, state: 'approved', can_decide: false, version: 3 })
    const { review } = createReview({ getRequest })
    review.rows.value = [{ id: 1, state: 'pending', can_decide: true, version: 1 }]
    review.openDecision(review.rows.value[0], 'approve')
    review.decisionForm.rationale = 'draft'

    await expect(review.reloadDecisionBaseline()).resolves.toMatchObject({ status: 'reloaded' })
    expect(review.decidingRequest.value.version).toBe(2)
    expect(review.decisionForm.rationale).toBe('')

    await expect(review.reloadDecisionBaseline()).resolves.toMatchObject({ status: 'already_processed' })
    expect(review.decisionDialog.value).toBe(false)
    expect(review.rows.value[0].version).toBe(3)
  })

  it('submits an approval payload and preserves the dialog on version conflict', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const decideRequest = vi.fn()
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(conflict)
    const { review } = createReview({ decideRequest })
    const row = { id: 1, state: 'pending', can_decide: true, version: 4, requested_expires_at: '2026-09-20T00:00:00Z' }

    review.openDecision(row, 'approve')
    review.decisionForm.rationale = ' approved for incident response '
    await expect(review.submitDecision()).resolves.toEqual({ status: 'succeeded', decision: 'approve' })
    expect(decideRequest).toHaveBeenNthCalledWith(1, 1, {
      version: 4,
      decision: 'approve',
      expires_at: '2026-09-20T00:00:00Z',
      rationale: 'approved for incident response'
    })
    expect(review.decisionDialog.value).toBe(false)
    expect(review.page.value).toBe(1)

    review.openDecision(row, 'reject')
    review.decisionForm.rationale = 'insufficient basis'
    await expect(review.submitDecision()).resolves.toMatchObject({ status: 'version_conflict' })
    expect(review.decisionConflict.value).toBe(true)
    expect(review.decisionDialog.value).toBe(true)
    expect(decideRequest).toHaveBeenNthCalledWith(2, 1, expect.objectContaining({ decision: 'reject', expires_at: undefined }))
  })

  it('closes an expired decision and invalidates in-flight work on disposal', async () => {
    const expired = { response: { status: 409, data: { error_code: 'protection_access_request_expired' } } }
    let resolveQueue
    const listRequests = vi.fn(() => new Promise(resolve => { resolveQueue = resolve }))
    const { review } = createReview({ listRequests, decideRequest: vi.fn().mockRejectedValue(expired) })
    const row = { id: 1, state: 'pending', can_decide: true, version: 1 }
    review.openDecision(row, 'reject')

    await expect(review.submitDecision()).resolves.toMatchObject({ status: 'expired' })
    expect(review.decisionDialog.value).toBe(false)

    const load = review.loadQueue()
    review.dispose()
    resolveQueue({ data: [{ id: 2 }], total: 1, total_pages: 1 })
    await expect(load).resolves.toBe(false)
    expect(review.rows.value).toEqual([])
    expect(review.loading.value).toBe(false)
  })
})
