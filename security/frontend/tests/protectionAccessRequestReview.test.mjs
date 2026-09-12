import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionAccessRequestReview } from '../src/composables/useProtectionAccessRequestReview.mjs'

function createReview(overrides = {}) {
  const dependencies = {
    canReview: true,
    listRequests: vi.fn().mockResolvedValue({ data: [{ id: 1 }], total: 1, total_pages: 1 }),
    getRequest: vi.fn(),
    decideRequest: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    t: vi.fn(key => key),
    onQueueLoadError: vi.fn(),
    onDecisionReloaded: vi.fn(),
    onAlreadyProcessed: vi.fn(),
    onDecisionLoadError: vi.fn(),
    onDecisionSucceeded: vi.fn(),
    onDecisionConflict: vi.fn(),
    onDecisionExpired: vi.fn(),
    onDecisionError: vi.fn(),
    ...overrides
  }
  return { dependencies, review: useProtectionAccessRequestReview(dependencies) }
}

function allowDecisionValidation(review) {
  review.decisionFormRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
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
    const { dependencies, review } = createReview({ getRequest })
    review.rows.value = [{ id: 1, state: 'pending', can_decide: true, version: 1 }]
    review.decisionFormRef.value = { clearValidate: vi.fn() }
    review.decisionCancelButton.value = { focus: vi.fn() }
    review.openDecision(review.rows.value[0], 'approve')
    review.decisionForm.rationale = 'draft'

    await expect(review.reloadDecisionBaseline()).resolves.toMatchObject({ status: 'reloaded' })
    await nextTick()
    expect(review.decidingRequest.value.version).toBe(2)
    expect(review.decisionForm.rationale).toBe('')
    expect(dependencies.onDecisionReloaded).toHaveBeenCalledOnce()
    expect(review.decisionFormRef.value.clearValidate).toHaveBeenCalledOnce()
    expect(review.decisionCancelButton.value.focus).toHaveBeenCalledOnce()

    await expect(review.reloadDecisionBaseline()).resolves.toMatchObject({ status: 'already_processed' })
    expect(review.decisionDialog.value).toBe(false)
    expect(dependencies.listRequests).toHaveBeenCalledOnce()
    expect(dependencies.onAlreadyProcessed).toHaveBeenCalledOnce()
  })

  it('submits one approval payload against the version captured when opened', async () => {
    let resolveValidation
    let resolveCollectionRefresh
    const decideRequest = vi.fn().mockResolvedValue({})
    const refreshCollection = vi.fn(() => new Promise(resolve => { resolveCollectionRefresh = resolve }))
    const { dependencies, review } = createReview({ decideRequest, refreshCollection })
    const row = { id: 1, state: 'pending', can_decide: true, version: 4, requested_expires_at: '2026-09-20T00:00:00Z' }
    review.decisionFormRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }

    review.openDecision(row, 'approve')
    review.decisionForm.rationale = ' approved for incident response '
    row.version = 9
    const firstSubmit = review.submitDecision()
    const secondSubmit = review.submitDecision()
    resolveValidation(true)
    await vi.waitFor(() => expect(refreshCollection).toHaveBeenCalledOnce())
    expect(review.decisionDialog.value).toBe(false)
    review.closeDecision()
    resolveCollectionRefresh(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'succeeded', decision: 'approve' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(decideRequest).toHaveBeenCalledOnce()
    expect(decideRequest).toHaveBeenCalledWith(1, {
      version: 4,
      decision: 'approve',
      expires_at: '2026-09-20T00:00:00Z',
      rationale: 'approved for incident response'
    })
    expect(dependencies.listRequests).toHaveBeenCalledWith(expect.objectContaining({ page: 1 }))
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onDecisionSucceeded).toHaveBeenCalledWith('approve')
    expect(review.decisionDialog.value).toBe(false)
    expect(review.page.value).toBe(1)
  })

  it('preserves a rejection rationale on version conflict', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const decideRequest = vi.fn().mockRejectedValue(conflict)
    const { dependencies, review } = createReview({ decideRequest })
    const row = { id: 1, state: 'pending', can_decide: true, version: 4, requested_expires_at: '2026-09-20T00:00:00Z' }
    allowDecisionValidation(review)
    review.openDecision(row, 'reject')
    review.decisionForm.rationale = 'insufficient basis'
    await expect(review.submitDecision()).resolves.toMatchObject({ status: 'version_conflict' })
    expect(review.decisionConflict.value).toBe(true)
    expect(review.decisionDialog.value).toBe(true)
    expect(review.decisionForm.rationale).toBe('insufficient basis')
    expect(decideRequest).toHaveBeenCalledWith(1, expect.objectContaining({ decision: 'reject', expires_at: undefined }))
    expect(dependencies.onDecisionConflict).toHaveBeenCalledOnce()
  })

  it('closes an expired decision and invalidates in-flight work on disposal', async () => {
    const expired = { response: { status: 409, data: { error_code: 'protection_access_request_expired' } } }
    const expiredSession = createReview({ decideRequest: vi.fn().mockRejectedValue(expired) })
    const row = { id: 1, state: 'pending', can_decide: true, version: 1 }
    allowDecisionValidation(expiredSession.review)
    expiredSession.review.openDecision(row, 'reject')

    await expect(expiredSession.review.submitDecision()).resolves.toMatchObject({ status: 'expired' })
    expect(expiredSession.review.decisionDialog.value).toBe(false)
    expect(expiredSession.dependencies.onDecisionExpired).toHaveBeenCalledOnce()

    let resolveQueue
    const listRequests = vi.fn(() => new Promise(resolve => { resolveQueue = resolve }))
    const { review } = createReview({ listRequests })

    const load = review.loadQueue()
    review.dispose()
    resolveQueue({ data: [{ id: 2 }], total: 1, total_pages: 1 })
    await expect(load).resolves.toBe(false)
    expect(review.rows.value).toEqual([])
    expect(review.loading.value).toBe(false)
  })

  it('does not send a decision when required validation fails', async () => {
    const decideRequest = vi.fn()
    const { review } = createReview({ decideRequest })
    review.decisionFormRef.value = { validate: vi.fn().mockResolvedValue(false) }
    review.openDecision({ id: 1, state: 'pending', can_decide: true, version: 1 }, 'approve')

    await expect(review.submitDecision()).resolves.toEqual({ status: 'invalid' })
    expect(decideRequest).not.toHaveBeenCalled()
    expect(review.decisionSaving.value).toBe(false)
    expect(review.decisionDialog.value).toBe(true)
  })

  it('owns required validation, presentation, and safe initial focus', async () => {
    const { review } = createReview()
    review.decisionFormRef.value = { clearValidate: vi.fn() }
    review.decisionCancelButton.value = { $el: { focus: vi.fn() } }
    const row = { id: 1, state: 'pending', can_decide: true, version: 1 }

    expect(review.openDecision(row, 'reject')).toBe(true)
    expect(review.decidingRequest.value).toMatchObject(row)
    expect(review.decidingRequest.value).not.toBe(row)
    expect(review.decisionRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })
    expect(review.decisionTitle.value).toBe('security.accessRequest.reject')
    expect(review.decisionHint.value).toBe('security.accessRequest.rejectHint')
    expect(review.decisionConfirmLabel.value).toBe('security.accessRequest.confirmActions.reject')

    review.focusDecisionCancel()
    await nextTick()
    expect(review.decisionFormRef.value.clearValidate).toHaveBeenCalledOnce()
    expect(review.decisionCancelButton.value.$el.focus).toHaveBeenCalledOnce()
  })

  it('reports decision load and submit failures without closing the dialog', async () => {
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const { dependencies, review } = createReview({
      getRequest: vi.fn().mockRejectedValue(loadError),
      decideRequest: vi.fn().mockRejectedValue(submitError)
    })
    allowDecisionValidation(review)
    review.openDecision({ id: 1, state: 'pending', can_decide: true, version: 1 }, 'approve')

    await expect(review.reloadDecisionBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    expect(dependencies.onDecisionLoadError).toHaveBeenCalledWith(loadError)
    await expect(review.submitDecision()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(dependencies.onDecisionError).toHaveBeenCalledWith(submitError)
    expect(review.decisionDialog.value).toBe(true)
  })
})
