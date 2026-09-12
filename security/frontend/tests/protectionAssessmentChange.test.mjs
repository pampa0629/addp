import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionAssessmentChange } from '../src/composables/useProtectionAssessmentChange.mjs'

const assessment = {
  id: 'assessment-1',
  version: 2,
  component_key: 'customers.email',
  current: {
    conclusion: 'sensitive',
    sensitive_data_type_id: 10,
    security_grade_id: 20
  }
}

function createAssessmentChange(overrides = {}) {
  const dependencies = {
    loadDefinitions: vi.fn().mockResolvedValue(true),
    activeGradesForType: vi.fn(() => [{ id: 20 }, { id: 21 }]),
    defaultGradeID: vi.fn(() => 20),
    getAssessment: vi.fn().mockResolvedValue(assessment),
    reviseAssessment: vi.fn().mockResolvedValue({ ...assessment, version: 3 }),
    revokeAssessment: vi.fn().mockResolvedValue({}),
    replaceAssessment: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    refreshGovernance: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    t: vi.fn(key => key),
    onDefinitionsError: vi.fn(),
    onRevisionReloaded: vi.fn(),
    onRevokeReloaded: vi.fn(),
    onAlreadyRevoked: vi.fn(),
    onLoadError: vi.fn(),
    onRevised: vi.fn(),
    onRevoked: vi.fn(),
    onRevisionConflict: vi.fn(),
    onRevokeConflict: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    assessmentChange: useProtectionAssessmentChange(dependencies)
  }
}

function allowRevisionValidation(assessmentChange) {
  assessmentChange.revisionFormRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
}

function allowRevokeValidation(assessmentChange) {
  assessmentChange.revokeFormRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
}

describe('protection assessment change sessions', () => {
  it('opens revision from the formal assessment baseline and owns required field validation', async () => {
    const { dependencies, assessmentChange } = createAssessmentChange()
    assessmentChange.revisionFormRef.value = { clearValidate: vi.fn() }
    assessmentChange.revisionRationaleInput.value = { focus: vi.fn() }

    await expect(assessmentChange.openRevision(assessment)).resolves.toEqual({ status: 'opened' })

    expect(dependencies.loadDefinitions).toHaveBeenCalledOnce()
    expect(assessmentChange.revisionDialog.value).toBe(true)
    expect(assessmentChange.revisionAssessment.value).toEqual(assessment)
    expect(assessmentChange.revisionAssessment.value).not.toBe(assessment)
    expect(assessmentChange.revisionForm).toMatchObject({
      sensitiveDataTypeID: '10',
      securityGradeID: '20',
      rationale: ''
    })
    expect(assessmentChange.revisionRules.value.sensitiveDataTypeID[0].required).toBe(true)
    expect(assessmentChange.revisionRules.value.securityGradeID[0].required).toBe(true)
    expect(assessmentChange.revisionRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })

    assessmentChange.focusRevisionRationale()
    await nextTick()
    expect(assessmentChange.revisionFormRef.value.clearValidate).toHaveBeenCalledOnce()
    expect(assessmentChange.revisionRationaleInput.value.focus).toHaveBeenCalledOnce()
  })

  it('falls back to the sensitive type default when the previous grade is no longer active', async () => {
    const { dependencies, assessmentChange } = createAssessmentChange({
      activeGradesForType: vi.fn(() => [{ id: 21 }]),
      defaultGradeID: vi.fn(() => 21)
    })

    await assessmentChange.openRevision(assessment)

    expect(assessmentChange.revisionForm.securityGradeID).toBe('21')
    assessmentChange.applyRevisionDefaultGrade('11')
    expect(dependencies.defaultGradeID).toHaveBeenLastCalledWith('11')
  })

  it('submits one canonical versioned revision while asynchronous validation is pending', async () => {
    let resolveValidation
    const openedAssessment = { ...assessment, current: { ...assessment.current } }
    const revised = { ...assessment, version: 3 }
    const { dependencies, assessmentChange } = createAssessmentChange({
      reviseAssessment: vi.fn().mockResolvedValue(revised)
    })
    assessmentChange.revisionFormRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }
    await assessmentChange.openRevision(openedAssessment)
    assessmentChange.revisionForm.sensitiveDataTypeID = '11'
    assessmentChange.revisionForm.securityGradeID = '21'
    assessmentChange.revisionForm.rationale = ' revised by owner '
    openedAssessment.version = 9
    openedAssessment.current.security_grade_id = 99

    const firstSubmit = assessmentChange.submitRevision()
    const secondSubmit = assessmentChange.submitRevision()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'revised', assessment: revised })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.reviseAssessment).toHaveBeenCalledOnce()
    expect(dependencies.reviseAssessment).toHaveBeenCalledWith('assessment-1', {
      version: 2,
      sensitive_data_type_id: 11,
      security_grade_id: 21,
      rationale: 'revised by owner'
    })
    expect(dependencies.replaceAssessment).toHaveBeenCalledWith(revised)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.refreshGovernance).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onRevised).toHaveBeenCalledOnce()
  })

  it('keeps revision input after a conflict and replaces it only after authoritative reload', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latest = {
      ...assessment,
      version: 4,
      current: { ...assessment.current, sensitive_data_type_id: 11, security_grade_id: 21 }
    }
    const { dependencies, assessmentChange } = createAssessmentChange({
      reviseAssessment: vi.fn().mockRejectedValue(conflict),
      getAssessment: vi.fn().mockResolvedValue(latest)
    })
    allowRevisionValidation(assessmentChange)
    await assessmentChange.openRevision(assessment)
    assessmentChange.revisionForm.rationale = 'preserve this input'

    await expect(assessmentChange.submitRevision()).resolves.toEqual({ status: 'version_conflict', error: conflict })
    expect(assessmentChange.revisionConflict.value).toBe(true)
    expect(assessmentChange.revisionForm.rationale).toBe('preserve this input')

    await expect(assessmentChange.reloadRevisionBaseline()).resolves.toEqual({ status: 'reloaded', assessment: latest })
    expect(dependencies.getAssessment).toHaveBeenCalledWith('assessment-1')
    expect(dependencies.replaceAssessment).toHaveBeenCalledWith(latest)
    expect(assessmentChange.revisionAssessment.value.version).toBe(4)
    expect(assessmentChange.revisionForm).toMatchObject({ sensitiveDataTypeID: '11', securityGradeID: '21', rationale: '' })
    expect(assessmentChange.revisionConflict.value).toBe(false)
    expect(dependencies.onRevisionReloaded).toHaveBeenCalledOnce()
  })

  it('opens revoke only for a sensitive conclusion and focuses the safe action', async () => {
    const { assessmentChange } = createAssessmentChange()
    assessmentChange.revokeFormRef.value = { clearValidate: vi.fn() }
    assessmentChange.revokeCancelButton.value = { $el: { focus: vi.fn() } }

    expect(assessmentChange.openRevoke(assessment)).toBe(true)
    expect(assessmentChange.revokeDialog.value).toBe(true)
    expect(assessmentChange.revokeAssessment.value).toEqual(assessment)
    expect(assessmentChange.revokeAssessment.value).not.toBe(assessment)
    expect(assessmentChange.revokeRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })
    assessmentChange.focusRevokeCancel()
    await nextTick()
    expect(assessmentChange.revokeFormRef.value.clearValidate).toHaveBeenCalledOnce()
    expect(assessmentChange.revokeCancelButton.value.$el.focus).toHaveBeenCalledOnce()

    assessmentChange.closeRevoke()
    expect(assessmentChange.openRevoke({ ...assessment, current: { conclusion: 'not_sensitive' } })).toBe(false)
  })

  it('submits one versioned revoke command while asynchronous validation is pending', async () => {
    let resolveValidation
    const { dependencies, assessmentChange } = createAssessmentChange()
    assessmentChange.revokeFormRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }
    assessmentChange.openRevoke(assessment)
    assessmentChange.revokeForm.rationale = ' classification corrected '

    const firstSubmit = assessmentChange.submitRevoke()
    const secondSubmit = assessmentChange.submitRevoke()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'revoked' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.revokeAssessment).toHaveBeenCalledOnce()
    expect(dependencies.revokeAssessment).toHaveBeenCalledWith('assessment-1', {
      version: 2,
      rationale: 'classification corrected'
    })
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.refreshGovernance).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onRevoked).toHaveBeenCalledOnce()
  })

  it('preserves revoke rationale on conflict and clears it after authoritative reload', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latest = { ...assessment, version: 5 }
    const { dependencies, assessmentChange } = createAssessmentChange({
      revokeAssessment: vi.fn().mockRejectedValue(conflict),
      getAssessment: vi.fn().mockResolvedValue(latest)
    })
    allowRevokeValidation(assessmentChange)
    assessmentChange.openRevoke(assessment)
    assessmentChange.revokeForm.rationale = 'preserve revoke input'

    await expect(assessmentChange.submitRevoke()).resolves.toEqual({ status: 'version_conflict', error: conflict })
    expect(assessmentChange.revokeConflict.value).toBe(true)
    expect(assessmentChange.revokeForm.rationale).toBe('preserve revoke input')

    await expect(assessmentChange.reloadRevokeBaseline()).resolves.toEqual({ status: 'reloaded', assessment: latest })
    expect(assessmentChange.revokeAssessment.value.version).toBe(5)
    expect(assessmentChange.revokeForm.rationale).toBe('')
    expect(assessmentChange.revokeConflict.value).toBe(false)
    expect(dependencies.onRevokeReloaded).toHaveBeenCalledOnce()
  })

  it('closes revoke when the authoritative assessment is already not sensitive', async () => {
    const latest = { ...assessment, version: 5, current: { conclusion: 'not_sensitive' } }
    const { dependencies, assessmentChange } = createAssessmentChange({
      getAssessment: vi.fn().mockResolvedValue(latest)
    })
    assessmentChange.openRevoke(assessment)

    await expect(assessmentChange.reloadRevokeBaseline()).resolves.toEqual({ status: 'already_revoked', assessment: latest })

    expect(dependencies.replaceAssessment).toHaveBeenCalledWith(latest)
    expect(assessmentChange.revokeDialog.value).toBe(false)
    expect(dependencies.onAlreadyRevoked).toHaveBeenCalledOnce()
  })

  it('reports dependency failures and invalidates pending commands on close or disposal', async () => {
    const definitionsError = new Error('definitions failed')
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const definitionsFailed = createAssessmentChange({ loadDefinitions: vi.fn().mockRejectedValue(definitionsError) })
    await expect(definitionsFailed.assessmentChange.openRevision(assessment)).resolves.toEqual({ status: 'failed', error: definitionsError })
    expect(definitionsFailed.dependencies.onDefinitionsError).toHaveBeenCalledWith(definitionsError)

    const failed = createAssessmentChange({
      getAssessment: vi.fn().mockRejectedValue(loadError),
      reviseAssessment: vi.fn().mockRejectedValue(submitError)
    })
    allowRevisionValidation(failed.assessmentChange)
    await failed.assessmentChange.openRevision(assessment)
    await expect(failed.assessmentChange.reloadRevisionBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    await expect(failed.assessmentChange.submitRevision()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(failed.dependencies.onLoadError).toHaveBeenCalledWith(loadError)
    expect(failed.dependencies.onSubmitError).toHaveBeenCalledWith(submitError)

    let resolveRevision
    const pending = createAssessmentChange({
      reviseAssessment: vi.fn(() => new Promise(resolve => { resolveRevision = resolve }))
    })
    allowRevisionValidation(pending.assessmentChange)
    await pending.assessmentChange.openRevision(assessment)
    const submission = pending.assessmentChange.submitRevision()
    await vi.waitFor(() => expect(resolveRevision).toBeTypeOf('function'))
    pending.assessmentChange.closeRevision()
    resolveRevision({ ...assessment, version: 3 })
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pending.dependencies.refreshCollection).not.toHaveBeenCalled()

    let resolveReload
    const disposed = createAssessmentChange({
      getAssessment: vi.fn(() => new Promise(resolve => { resolveReload = resolve }))
    })
    disposed.assessmentChange.openRevoke(assessment)
    const reload = disposed.assessmentChange.reloadRevokeBaseline()
    disposed.assessmentChange.dispose()
    resolveReload(assessment)
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(disposed.dependencies.replaceAssessment).not.toHaveBeenCalled()
  })
})
