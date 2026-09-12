import { describe, expect, it, vi } from 'vitest'
import { useProtectionAssessmentWorkspace } from '../src/composables/useProtectionAssessmentWorkspace.mjs'

const enrollment = { id: 'enrollment-1', version: 4, state: 'active' }
const componentResponse = {
  data: [{ component: { key: 'customers.email', value_type: 'string' } }]
}
const assessment = {
  id: 'assessment-1',
  current_revision: 2,
  component_key: 'customers.email',
  history: [{ id: 'revision-2', revision: 2 }, { id: 'revision-1', revision: 1 }]
}

function createWorkspace(overrides = {}) {
  const state = { enrollment }
  const dependencies = {
    getEnrollment: vi.fn(() => state.enrollment),
    loadComponents: vi.fn().mockResolvedValue(componentResponse),
    loadDefinitions: vi.fn().mockResolvedValue(true),
    refreshAssessments: vi.fn().mockResolvedValue(true),
    createAssessment: vi.fn().mockResolvedValue(assessment),
    getAssessment: vi.fn().mockResolvedValue(assessment),
    refreshCollection: vi.fn().mockResolvedValue(true),
    refreshGovernance: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    defaultGradeID: vi.fn(() => 20),
    t: vi.fn(key => key),
    onComponentsLoadError: vi.fn(),
    onDesignationSaved: vi.fn(),
    onDesignationError: vi.fn(),
    onHistoryLoadError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    state,
    workspace: useProtectionAssessmentWorkspace(dependencies)
  }
}

function allowDesignationValidation(workspace) {
  workspace.designationDialogRef.value = { validate: vi.fn().mockResolvedValue(true), focusPrimary: vi.fn() }
}

describe('protection assessment workspace sessions', () => {
  it('loads eligible components and definitions into a required designation form', async () => {
    const { dependencies, workspace } = createWorkspace()

    await expect(workspace.openDesignation()).resolves.toEqual({
      status: 'opened',
      components: componentResponse.data
    })

    expect(dependencies.loadComponents).toHaveBeenCalledWith('enrollment-1')
    expect(dependencies.loadDefinitions).toHaveBeenCalledOnce()
    expect(dependencies.refreshAssessments).toHaveBeenCalledOnce()
    expect(workspace.designationDialog.value).toBe(true)
    expect(workspace.componentOptions.value).toEqual(componentResponse.data)
    expect(workspace.designationRules.value.componentKey[0].required).toBe(true)
    expect(workspace.designationRules.value.sensitiveDataTypeID[0].required).toBe(true)
    expect(workspace.designationRules.value.securityGradeID[0].required).toBe(true)
    expect(workspace.designationRules.value.rationale[0]).toMatchObject({ required: true, whitespace: true })

    workspace.applyDesignationDefaultGrade('10')
    expect(dependencies.defaultGradeID).toHaveBeenCalledWith('10')
    expect(workspace.designationForm.securityGradeID).toBe('20')
  })

  it('submits one designation against the enrollment version captured when opened', async () => {
    let resolveValidation
    const openedEnrollment = { ...enrollment }
    const { dependencies, state, workspace } = createWorkspace({ getEnrollment: vi.fn(() => openedEnrollment) })
    workspace.designationDialogRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      focusPrimary: vi.fn()
    }
    await workspace.openDesignation()
    workspace.designationForm.componentKey = 'customers.email'
    workspace.designationForm.sensitiveDataTypeID = '10'
    workspace.designationForm.securityGradeID = '20'
    workspace.designationForm.rationale = ' business confirmed '
    openedEnrollment.version = 9
    state.enrollment = openedEnrollment

    const firstSubmit = workspace.submitDesignation()
    const secondSubmit = workspace.submitDesignation()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'designated', assessment })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.createAssessment).toHaveBeenCalledOnce()
    expect(dependencies.createAssessment).toHaveBeenCalledWith({
      enrollment_id: 'enrollment-1',
      enrollment_version: 4,
      component_key: 'customers.email',
      sensitive_data_type_id: 10,
      security_grade_id: 20,
      rationale: 'business confirmed'
    })
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.refreshGovernance).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onDesignationSaved).toHaveBeenCalledOnce()
    expect(workspace.designationDialog.value).toBe(false)
  })

  it('keeps invalid and failed designation forms available for correction', async () => {
    const submitError = new Error('submit failed')
    const { dependencies, workspace } = createWorkspace({ createAssessment: vi.fn().mockRejectedValue(submitError) })
    workspace.designationDialogRef.value = { validate: vi.fn().mockResolvedValue(false) }
    await workspace.openDesignation()

    await expect(workspace.submitDesignation()).resolves.toEqual({ status: 'invalid' })
    expect(dependencies.createAssessment).not.toHaveBeenCalled()

    allowDesignationValidation(workspace)
    await expect(workspace.submitDesignation()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(workspace.designationDialog.value).toBe(true)
    expect(dependencies.onDesignationError).toHaveBeenCalledWith(submitError)
  })

  it('reports component loading failures and ignores results after the dialog closes', async () => {
    const loadError = new Error('components failed')
    const failed = createWorkspace({ loadComponents: vi.fn().mockRejectedValue(loadError) })

    await expect(failed.workspace.openDesignation()).resolves.toEqual({ status: 'failed', error: loadError })
    expect(failed.workspace.designationDialog.value).toBe(true)
    expect(failed.dependencies.onComponentsLoadError).toHaveBeenCalledWith(loadError)

    let resolveComponents
    const pending = createWorkspace({
      loadComponents: vi.fn(() => new Promise(resolve => { resolveComponents = resolve }))
    })
    const opening = pending.workspace.openDesignation()
    pending.workspace.closeDesignation()
    resolveComponents(componentResponse)
    await expect(opening).resolves.toEqual({ status: 'stale' })
    expect(pending.workspace.componentOptions.value).toEqual([])
    expect(pending.workspace.designationDialog.value).toBe(false)
  })

  it('loads immutable history and identifies the current revision', async () => {
    const { dependencies, workspace } = createWorkspace()

    await expect(workspace.openHistory({ id: 'assessment-1' })).resolves.toEqual({
      status: 'loaded',
      assessment
    })

    expect(dependencies.getAssessment).toHaveBeenCalledWith('assessment-1')
    expect(workspace.historyDialog.value).toBe(true)
    expect(workspace.history.value).toEqual(assessment)
  })

  it('keeps only the latest history request result', async () => {
    let resolveFirst
    let resolveSecond
    const first = new Promise(resolve => { resolveFirst = resolve })
    const second = new Promise(resolve => { resolveSecond = resolve })
    const { workspace } = createWorkspace({
      getAssessment: vi.fn()
        .mockReturnValueOnce(first)
        .mockReturnValueOnce(second)
    })

    const firstLoad = workspace.openHistory({ id: 'assessment-1' })
    const secondLoad = workspace.openHistory({ id: 'assessment-2' })
    const latest = { ...assessment, id: 'assessment-2' }
    resolveFirst(assessment)
    resolveSecond(latest)

    await expect(firstLoad).resolves.toEqual({ status: 'stale' })
    await expect(secondLoad).resolves.toEqual({ status: 'loaded', assessment: latest })
    expect(workspace.history.value).toEqual(latest)
  })

  it('closes failed history loads and reports the error', async () => {
    const loadError = new Error('history failed')
    const { dependencies, workspace } = createWorkspace({ getAssessment: vi.fn().mockRejectedValue(loadError) })

    await expect(workspace.openHistory({ id: 'assessment-1' })).resolves.toEqual({ status: 'failed', error: loadError })

    expect(workspace.historyDialog.value).toBe(false)
    expect(workspace.history.value).toBeNull()
    expect(dependencies.onHistoryLoadError).toHaveBeenCalledWith(loadError)
  })

  it('invalidates pending designation submits and history loads on close or disposal', async () => {
    let resolveCreate
    const pendingSubmit = createWorkspace({
      createAssessment: vi.fn(() => new Promise(resolve => { resolveCreate = resolve }))
    })
    allowDesignationValidation(pendingSubmit.workspace)
    await pendingSubmit.workspace.openDesignation()
    const submission = pendingSubmit.workspace.submitDesignation()
    await vi.waitFor(() => expect(resolveCreate).toBeTypeOf('function'))
    pendingSubmit.workspace.closeDesignation()
    resolveCreate(assessment)
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pendingSubmit.dependencies.refreshCollection).not.toHaveBeenCalled()

    let resolveHistory
    const pendingHistory = createWorkspace({
      getAssessment: vi.fn(() => new Promise(resolve => { resolveHistory = resolve }))
    })
    const loading = pendingHistory.workspace.openHistory({ id: 'assessment-1' })
    pendingHistory.workspace.dispose()
    resolveHistory(assessment)
    await expect(loading).resolves.toEqual({ status: 'stale' })
    expect(pendingHistory.workspace.history.value).toBeNull()
    expect(pendingHistory.workspace.historyLoading.value).toBe(false)
  })
})
