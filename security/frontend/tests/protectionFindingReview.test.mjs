import { nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionFindingReview } from '../src/composables/useProtectionFindingReview.mjs'

const sourceFinding = { id: 'finding-1', sensitive_data_type_id: 4 }

function createReview(overrides = {}) {
  const dependencies = {
    sensitiveTypes: ref([
      { id: 4, default_security_grade_id: 40 },
      { id: 5, default_security_grade_id: 50 }
    ]),
    loadDefinitions: vi.fn().mockResolvedValue(undefined),
    reviewFinding: vi.fn().mockResolvedValue(undefined),
    resolveContext: vi.fn(() => 'queue'),
    continueAfterReview: vi.fn().mockResolvedValue(null),
    t: vi.fn(key => key),
    onDefinitionsError: vi.fn(),
    onSubmitError: vi.fn(),
    onContinuationError: vi.fn(),
    onSaved: vi.fn(),
    onContinued: vi.fn(),
    ...overrides
  }
  return { dependencies, review: useProtectionFindingReview(dependencies) }
}

function allowValidation(review) {
  review.dialogRef.value = { validate: vi.fn().mockResolvedValue(true), focusPrimary: vi.fn() }
}

describe('protection finding review session', () => {
  it('opens with the source type default grade and records the workspace context', async () => {
    const { dependencies, review } = createReview()
    allowValidation(review)

    await expect(review.open(sourceFinding, 'adjust')).resolves.toBe(true)
    await nextTick()

    expect(dependencies.loadDefinitions).toHaveBeenCalledOnce()
    expect(review.dialog.value).toBe(true)
    expect(review.finding.value).toEqual(sourceFinding)
    expect(review.form).toEqual({
      decision: 'adjust',
      sensitiveDataTypeID: '4',
      securityGradeID: '40',
      rationale: ''
    })
    expect(review.dialogRef.value.focusPrimary).toHaveBeenCalled()

    review.applyDefaultGrade('5')
    expect(review.form.securityGradeID).toBe('50')
  })

  it('captures the workspace context before definitions finish loading', async () => {
    let resolveDefinitions
    let activeContext = 'queue'
    const { dependencies, review } = createReview({
      loadDefinitions: vi.fn(() => new Promise(resolve => { resolveDefinitions = resolve })),
      resolveContext: vi.fn(() => activeContext)
    })

    const opening = review.open(sourceFinding)
    activeContext = 'resource'
    resolveDefinitions()
    await opening
    allowValidation(review)
    await review.submit()

    expect(dependencies.continueAfterReview).toHaveBeenCalledWith(sourceFinding, 'queue')
  })

  it('submits the canonical payload and continues in the captured context', async () => {
    const nextFinding = { id: 'finding-2', sensitive_data_type_id: 5 }
    const { dependencies, review } = createReview({
      continueAfterReview: vi.fn().mockResolvedValue(nextFinding)
    })
    allowValidation(review)
    await review.open(sourceFinding, 'adjust')
    review.form.sensitiveDataTypeID = '5'
    review.form.securityGradeID = '50'
    review.form.rationale = ' corrected after review '

    await expect(review.submit()).resolves.toEqual({ status: 'continued', finding: nextFinding })

    expect(dependencies.reviewFinding).toHaveBeenCalledWith('finding-1', {
      decision: 'adjust',
      sensitive_data_type_id: 5,
      security_grade_id: 50,
      rationale: 'corrected after review'
    })
    expect(dependencies.continueAfterReview).toHaveBeenCalledWith(sourceFinding, 'queue')
    expect(review.finding.value).toEqual(nextFinding)
    expect(review.form.decision).toBe('confirm')
    expect(review.form.securityGradeID).toBe('50')
    expect(dependencies.onContinued).toHaveBeenCalledOnce()
    expect(dependencies.onSaved).not.toHaveBeenCalled()
  })

  it('closes the session when no pending candidate remains', async () => {
    const { dependencies, review } = createReview()
    allowValidation(review)
    await review.open(sourceFinding)

    await expect(review.submit()).resolves.toEqual({ status: 'saved' })

    expect(review.dialog.value).toBe(false)
    expect(review.finding.value).toBe(null)
    expect(dependencies.onSaved).toHaveBeenCalledOnce()
  })

  it('keeps invalid or failed submissions in the dialog', async () => {
    const submitError = new Error('submit failed')
    const { dependencies, review } = createReview({ reviewFinding: vi.fn().mockRejectedValue(submitError) })
    review.dialogRef.value = { validate: vi.fn().mockResolvedValue(false) }
    await review.open(sourceFinding)

    await expect(review.submit()).resolves.toEqual({ status: 'invalid' })
    expect(dependencies.reviewFinding).not.toHaveBeenCalled()

    allowValidation(review)
    await expect(review.submit()).resolves.toEqual({ status: 'failed' })
    expect(review.dialog.value).toBe(true)
    expect(review.finding.value).toEqual(sourceFinding)
    expect(review.saving.value).toBe(false)
    expect(dependencies.onSubmitError).toHaveBeenCalledWith(submitError)
  })

  it('prevents duplicate submissions while async validation is pending', async () => {
    let resolveValidation
    const { dependencies, review } = createReview()
    review.dialogRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      focusPrimary: vi.fn()
    }
    await review.open(sourceFinding)

    const firstSubmit = review.submit()
    const secondSubmit = review.submit()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'saved' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.reviewFinding).toHaveBeenCalledOnce()
  })

  it('closes after a continuation failure without reporting a false save', async () => {
    const continuationError = new Error('reload failed')
    const { dependencies, review } = createReview({
      continueAfterReview: vi.fn().mockRejectedValue(continuationError)
    })
    allowValidation(review)
    await review.open(sourceFinding)

    await expect(review.submit()).resolves.toEqual({ status: 'continuation_failed', error: continuationError })

    expect(review.dialog.value).toBe(false)
    expect(review.finding.value).toBe(null)
    expect(dependencies.onContinuationError).toHaveBeenCalledWith(continuationError)
    expect(dependencies.onSaved).not.toHaveBeenCalled()
  })

  it('ignores failed or stale definition loads and invalidates active work on disposal', async () => {
    const definitionError = new Error('definitions failed')
    const failed = createReview({ loadDefinitions: vi.fn().mockRejectedValue(definitionError) })
    await expect(failed.review.open(sourceFinding)).resolves.toBe(false)
    expect(failed.review.dialog.value).toBe(false)
    expect(failed.dependencies.onDefinitionsError).toHaveBeenCalledWith(definitionError)

    let resolveDefinitions
    const pending = createReview({
      loadDefinitions: vi.fn(() => new Promise(resolve => { resolveDefinitions = resolve }))
    })
    const opening = pending.review.open(sourceFinding)
    pending.review.dispose()
    resolveDefinitions()
    await expect(opening).resolves.toBe(false)
    expect(pending.review.dialog.value).toBe(false)
    expect(pending.dependencies.onDefinitionsError).not.toHaveBeenCalled()
  })
})
