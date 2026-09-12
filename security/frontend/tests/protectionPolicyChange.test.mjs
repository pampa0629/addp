import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionPolicyChange } from '../src/composables/useProtectionPolicyChange.mjs'

const assessment = {
  id: 'assessment-1',
  current: { conclusion: 'sensitive' }
}

const activePolicy = {
  id: 'policy-1',
  assessment_id: 'assessment-1',
  state: 'active',
  version: 2,
  current: { effect: 'suppress', rationale: 'existing rationale' }
}

function createPolicyChange(options = {}) {
  const state = {
    policy: Object.prototype.hasOwnProperty.call(options, 'currentPolicy')
      ? options.currentPolicy
      : activePolicy
  }
  const { currentPolicy: _currentPolicy, ...overrides } = options
  const dependencies = {
    policyForAssessment: vi.fn(() => state.policy),
    stricterEffects: vi.fn(() => ['suppress', 'deny']),
    getPolicy: vi.fn().mockResolvedValue(activePolicy),
    getAssessment: vi.fn().mockResolvedValue(assessment),
    listBaselines: vi.fn().mockResolvedValue([{ id: 'baseline-1' }]),
    applyLatestContext: vi.fn(context => { state.policy = context.policy }),
    createPolicy: vi.fn().mockResolvedValue({ id: 'policy-created' }),
    updatePolicy: vi.fn().mockResolvedValue({ id: 'policy-updated' }),
    revokePolicy: vi.fn().mockResolvedValue({ id: 'policy-revoked' }),
    refreshCollection: vi.fn().mockResolvedValue(true),
    refreshGovernance: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    t: vi.fn(key => key),
    onAlreadyStrictest: vi.fn(),
    onEditReloaded: vi.fn(),
    onRestoreReloaded: vi.fn(),
    onAlreadyRestored: vi.fn(),
    onLoadError: vi.fn(),
    onSaved: vi.fn(),
    onRestored: vi.fn(),
    onEditConflict: vi.fn(),
    onRestoreConflict: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    state,
    policyChange: useProtectionPolicyChange(dependencies)
  }
}

function allowEditValidation(policyChange) {
  policyChange.editFormRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
}

function allowRestoreValidation(policyChange) {
  policyChange.restoreFormRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
}

describe('protection policy change sessions', () => {
  it('opens tightening from the current policy baseline and rejects an already-strictest assessment', async () => {
    const { policyChange } = createPolicyChange()
    policyChange.editFormRef.value = { clearValidate: vi.fn() }

    expect(policyChange.openEdit(assessment)).toBe(true)
    expect(policyChange.editDialog.value).toBe(true)
    expect(policyChange.editAssessment.value).toEqual(assessment)
    expect(policyChange.editForm).toMatchObject({ effect: 'suppress', rationale: 'existing rationale' })
    expect(policyChange.editEffects.value).toEqual(['suppress', 'deny'])
    expect(policyChange.editRules.value.effect[0].required).toBe(true)
    expect(policyChange.editRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })
    policyChange.clearEditValidation()
    await nextTick()
    expect(policyChange.editFormRef.value.clearValidate).toHaveBeenCalledOnce()

    const strictest = createPolicyChange({ stricterEffects: vi.fn(() => []) })
    expect(strictest.policyChange.openEdit(assessment)).toBe(false)
    expect(strictest.dependencies.onAlreadyStrictest).toHaveBeenCalledOnce()
    expect(strictest.policyChange.editDialog.value).toBe(false)
  })

  it('creates one canonical manager preview policy while async validation is pending', async () => {
    let resolveValidation
    const { dependencies, policyChange } = createPolicyChange({ currentPolicy: null })
    policyChange.editFormRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }
    policyChange.openEdit(assessment)
    policyChange.editForm.effect = 'deny'
    policyChange.editForm.rationale = ' required by owner '

    const firstSubmit = policyChange.submitEdit()
    const secondSubmit = policyChange.submitEdit()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'saved', operation: 'created' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.createPolicy).toHaveBeenCalledOnce()
    expect(dependencies.createPolicy).toHaveBeenCalledWith({
      assessment_id: 'assessment-1',
      consumer_owner: 'manager',
      action: 'preview',
      effect: 'deny',
      rationale: 'required by owner'
    })
    expect(dependencies.updatePolicy).not.toHaveBeenCalled()
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.refreshGovernance).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onSaved).toHaveBeenCalledOnce()
  })

  it('keeps the opened policy version until the user explicitly reloads its baseline', async () => {
    const { dependencies, state, policyChange } = createPolicyChange()
    allowEditValidation(policyChange)
    policyChange.openEdit(assessment)
    policyChange.editForm.effect = 'deny'
    policyChange.editForm.rationale = 'confirmed against opened baseline'

    state.policy = { ...activePolicy, version: 9 }
    await expect(policyChange.submitEdit()).resolves.toEqual({ status: 'saved', operation: 'updated' })

    expect(dependencies.updatePolicy).toHaveBeenCalledWith('policy-1', {
      version: 2,
      effect: 'deny',
      rationale: 'confirmed against opened baseline'
    })
  })

  it('preserves an update after conflict and reloads all authoritative policy context before retry', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latestPolicy = {
      ...activePolicy,
      version: 3,
      current: { effect: 'suppress', rationale: 'latest rationale' }
    }
    const latestAssessment = { ...assessment, version: 6 }
    const { dependencies, policyChange } = createPolicyChange({
      updatePolicy: vi.fn().mockRejectedValueOnce(conflict).mockResolvedValueOnce({}),
      getPolicy: vi.fn().mockResolvedValue(latestPolicy),
      getAssessment: vi.fn().mockResolvedValue(latestAssessment)
    })
    allowEditValidation(policyChange)
    policyChange.openEdit(assessment)
    policyChange.editForm.effect = 'deny'
    policyChange.editForm.rationale = 'preserve this input'

    await expect(policyChange.submitEdit()).resolves.toEqual({ status: 'version_conflict', error: conflict })
    expect(policyChange.editConflict.value).toBe(true)
    expect(policyChange.editForm).toMatchObject({ effect: 'deny', rationale: 'preserve this input' })

    await expect(policyChange.reloadEditBaseline()).resolves.toMatchObject({ status: 'reloaded' })
    expect(dependencies.getPolicy).toHaveBeenCalledWith('policy-1')
    expect(dependencies.getAssessment).toHaveBeenCalledWith('assessment-1')
    expect(dependencies.listBaselines).toHaveBeenCalledOnce()
    expect(dependencies.applyLatestContext).toHaveBeenCalledWith({
      policy: latestPolicy,
      assessment: latestAssessment,
      baselines: [{ id: 'baseline-1' }]
    })
    expect(policyChange.editConflict.value).toBe(false)
    expect(policyChange.editForm).toMatchObject({ effect: 'suppress', rationale: 'latest rationale' })
    expect(dependencies.onEditReloaded).toHaveBeenCalledOnce()

    policyChange.editForm.effect = 'deny'
    policyChange.editForm.rationale = 'retry with latest'
    await expect(policyChange.submitEdit()).resolves.toEqual({ status: 'saved', operation: 'updated' })
    expect(dependencies.updatePolicy).toHaveBeenLastCalledWith('policy-1', {
      version: 3,
      effect: 'deny',
      rationale: 'retry with latest'
    })
  })

  it('opens restore with a required rationale and focuses the safe action', async () => {
    const { policyChange } = createPolicyChange()
    policyChange.restoreFormRef.value = { clearValidate: vi.fn() }
    policyChange.restoreCancelButton.value = { focus: vi.fn() }

    expect(policyChange.openRestore(assessment)).toBe(true)
    expect(policyChange.restoreDialog.value).toBe(true)
    expect(policyChange.restoreAssessment.value).toEqual(assessment)
    expect(policyChange.restoreRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })
    policyChange.focusRestoreCancel()
    await nextTick()
    expect(policyChange.restoreFormRef.value.clearValidate).toHaveBeenCalledOnce()
    expect(policyChange.restoreCancelButton.value.focus).toHaveBeenCalledOnce()

    const inactive = createPolicyChange({ currentPolicy: { ...activePolicy, state: 'revoked' } })
    expect(inactive.policyChange.openRestore(assessment)).toBe(false)
  })

  it('preserves restore rationale on conflict and clears it after authoritative reload', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latestPolicy = { ...activePolicy, version: 4 }
    const { dependencies, policyChange } = createPolicyChange({
      revokePolicy: vi.fn().mockRejectedValue(conflict),
      getPolicy: vi.fn().mockResolvedValue(latestPolicy)
    })
    allowRestoreValidation(policyChange)
    policyChange.restoreCancelButton.value = { focus: vi.fn() }
    policyChange.openRestore(assessment)
    policyChange.restoreForm.rationale = 'preserve restore input'

    await expect(policyChange.submitRestore()).resolves.toEqual({ status: 'version_conflict', error: conflict })
    expect(policyChange.restoreConflict.value).toBe(true)
    expect(policyChange.restoreForm.rationale).toBe('preserve restore input')

    await expect(policyChange.reloadRestoreBaseline()).resolves.toMatchObject({ status: 'reloaded' })
    await nextTick()
    expect(policyChange.restoreAssessment.value).toEqual(assessment)
    expect(policyChange.restoreForm.rationale).toBe('')
    expect(policyChange.restoreConflict.value).toBe(false)
    expect(dependencies.onRestoreReloaded).toHaveBeenCalledOnce()
    expect(policyChange.restoreCancelButton.value.focus).toHaveBeenCalledOnce()
  })

  it('closes restore when the latest policy context is already inactive', async () => {
    const latestPolicy = { ...activePolicy, version: 4, state: 'revoked' }
    const { dependencies, policyChange } = createPolicyChange({
      getPolicy: vi.fn().mockResolvedValue(latestPolicy)
    })
    policyChange.openRestore(assessment)

    await expect(policyChange.reloadRestoreBaseline()).resolves.toMatchObject({ status: 'already_restored' })

    expect(policyChange.restoreDialog.value).toBe(false)
    expect(dependencies.applyLatestContext).toHaveBeenCalledOnce()
    expect(dependencies.onAlreadyRestored).toHaveBeenCalledOnce()
  })

  it('revokes the current policy through one versioned restore command', async () => {
    let resolveValidation
    const { dependencies, policyChange } = createPolicyChange()
    policyChange.restoreFormRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }
    policyChange.openRestore(assessment)
    policyChange.restoreForm.rationale = ' restore platform default '

    const firstSubmit = policyChange.submitRestore()
    const secondSubmit = policyChange.submitRestore()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'restored' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.revokePolicy).toHaveBeenCalledOnce()
    expect(dependencies.revokePolicy).toHaveBeenCalledWith('policy-1', {
      version: 2,
      rationale: 'restore platform default'
    })
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.refreshGovernance).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onRestored).toHaveBeenCalledOnce()
    expect(policyChange.restoreDialog.value).toBe(false)
  })

  it('reports load and submit failures and invalidates pending work on close or disposal', async () => {
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const failed = createPolicyChange({
      getPolicy: vi.fn().mockRejectedValue(loadError),
      updatePolicy: vi.fn().mockRejectedValue(submitError)
    })
    allowEditValidation(failed.policyChange)
    failed.policyChange.openEdit(assessment)
    await expect(failed.policyChange.reloadEditBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    await expect(failed.policyChange.submitEdit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(failed.dependencies.onLoadError).toHaveBeenCalledWith(loadError)
    expect(failed.dependencies.onSubmitError).toHaveBeenCalledWith(submitError)

    let resolveUpdate
    const pendingEdit = createPolicyChange({
      updatePolicy: vi.fn(() => new Promise(resolve => { resolveUpdate = resolve }))
    })
    allowEditValidation(pendingEdit.policyChange)
    pendingEdit.policyChange.openEdit(assessment)
    const submission = pendingEdit.policyChange.submitEdit()
    await vi.waitFor(() => expect(resolveUpdate).toBeTypeOf('function'))
    pendingEdit.policyChange.closeEdit()
    resolveUpdate({})
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pendingEdit.dependencies.refreshCollection).not.toHaveBeenCalled()

    let resolveRestoreReload
    const pendingRestore = createPolicyChange({
      getPolicy: vi.fn(() => new Promise(resolve => { resolveRestoreReload = resolve }))
    })
    pendingRestore.policyChange.openRestore(assessment)
    const reload = pendingRestore.policyChange.reloadRestoreBaseline()
    pendingRestore.policyChange.dispose()
    resolveRestoreReload(activePolicy)
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(pendingRestore.dependencies.applyLatestContext).not.toHaveBeenCalled()
    expect(pendingRestore.policyChange.restoreReloading.value).toBe(false)
  })
})
