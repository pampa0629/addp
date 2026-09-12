import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentReEnrollment } from '../src/composables/useProtectionEnrollmentReEnrollment.mjs'

const releasedEnrollment = {
  id: 'released-enrollment-1',
  version: 7,
  state: 'released',
  target_snapshot: { full_name: 'business.customers' }
}

const newEnrollment = {
  id: 'current-enrollment-2',
  version: 1,
  state: 'activating',
  target_snapshot: { full_name: 'business.customers' }
}

function createReEnrollment(overrides = {}) {
  const dependencies = {
    getEnrollment: vi.fn(),
    createReEnrollment: vi.fn().mockResolvedValue({
      source_enrollment_version: 8,
      enrollment: newEnrollment
    }),
    replaceEnrollment: vi.fn(),
    showCurrentCollection: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    onUnavailable: vi.fn(),
    onReloaded: vi.fn(),
    onLoadError: vi.fn(),
    onCreated: vi.fn(),
    onVersionConflict: vi.fn(),
    onAlreadyActive: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    reEnrollment: useProtectionEnrollmentReEnrollment(dependencies)
  }
}

describe('protection enrollment re-enrollment session', () => {
  it('only opens released lifecycles', () => {
    const { reEnrollment } = createReEnrollment()

    expect(reEnrollment.open(releasedEnrollment)).toBe(true)
    expect(reEnrollment.dialog.value).toBe(true)
    expect(reEnrollment.source.value).toEqual(releasedEnrollment)

    reEnrollment.close()
    expect(reEnrollment.dialog.value).toBe(false)
    expect(reEnrollment.source.value).toBeNull()
    expect(reEnrollment.open({ ...releasedEnrollment, state: 'active' })).toBe(false)
  })

  it('reloads the authoritative released lifecycle before reconfirming', async () => {
    const latest = { ...releasedEnrollment, version: 9 }
    const { dependencies, reEnrollment } = createReEnrollment({
      getEnrollment: vi.fn().mockResolvedValue(latest)
    })
    reEnrollment.dialogRef.value = { focusPrimary: vi.fn() }
    reEnrollment.open(releasedEnrollment)
    reEnrollment.conflict.value = true

    await expect(reEnrollment.reloadBaseline()).resolves.toMatchObject({ status: 'reloaded', enrollment: latest })
    await nextTick()

    expect(dependencies.getEnrollment).toHaveBeenCalledWith('released-enrollment-1')
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(reEnrollment.source.value).toEqual(latest)
    expect(reEnrollment.conflict.value).toBe(false)
    expect(dependencies.onReloaded).toHaveBeenCalledOnce()
    expect(reEnrollment.dialogRef.value.focusPrimary).toHaveBeenCalledOnce()
  })

  it('closes when the authoritative lifecycle is no longer released', async () => {
    const latest = { ...releasedEnrollment, version: 9, state: 'active' }
    const { dependencies, reEnrollment } = createReEnrollment({
      getEnrollment: vi.fn().mockResolvedValue(latest)
    })
    reEnrollment.open(releasedEnrollment)

    await expect(reEnrollment.reloadBaseline()).resolves.toMatchObject({ status: 'unavailable_state' })

    expect(reEnrollment.dialog.value).toBe(false)
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onUnavailable).toHaveBeenCalledOnce()
  })

  it('creates one versioned lifecycle and switches to its current collection', async () => {
    let resolveCommand
    const { dependencies, reEnrollment } = createReEnrollment({
      createReEnrollment: vi.fn(() => new Promise(resolve => { resolveCommand = resolve }))
    })
    reEnrollment.open(releasedEnrollment)

    const firstSubmit = reEnrollment.submit()
    const secondSubmit = reEnrollment.submit()
    resolveCommand({ source_enrollment_version: 10, enrollment: newEnrollment })

    await expect(firstSubmit).resolves.toMatchObject({ status: 'created' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.createReEnrollment).toHaveBeenCalledOnce()
    expect(dependencies.createReEnrollment).toHaveBeenCalledWith('released-enrollment-1', { version: 7 })
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith({ ...releasedEnrollment, version: 10 })
    expect(dependencies.showCurrentCollection).toHaveBeenCalledWith(newEnrollment)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onCreated).toHaveBeenCalledOnce()
    expect(reEnrollment.dialog.value).toBe(false)
  })

  it('preserves the released lifecycle after a version conflict', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const { dependencies, reEnrollment } = createReEnrollment({
      createReEnrollment: vi.fn().mockRejectedValue(conflict)
    })
    reEnrollment.open(releasedEnrollment)

    await expect(reEnrollment.submit()).resolves.toEqual({ status: 'version_conflict', error: conflict })

    expect(reEnrollment.dialog.value).toBe(true)
    expect(reEnrollment.source.value).toEqual(releasedEnrollment)
    expect(reEnrollment.conflict.value).toBe(true)
    expect(dependencies.onVersionConflict).toHaveBeenCalledOnce()
    expect(dependencies.showCurrentCollection).not.toHaveBeenCalled()
  })

  it('switches to the authoritative current collection when a lifecycle already exists', async () => {
    const alreadyActive = {
      response: { status: 409, data: { error_code: 'protection_enrollment_already_active' } }
    }
    const { dependencies, reEnrollment } = createReEnrollment({
      createReEnrollment: vi.fn().mockRejectedValue(alreadyActive)
    })
    reEnrollment.open(releasedEnrollment)

    await expect(reEnrollment.submit()).resolves.toEqual({ status: 'already_active', error: alreadyActive })

    expect(dependencies.showCurrentCollection).toHaveBeenCalledWith()
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onAlreadyActive).toHaveBeenCalledOnce()
    expect(reEnrollment.dialog.value).toBe(false)
  })

  it('reports actionable failures and invalidates pending work on close or disposal', async () => {
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const failed = createReEnrollment({
      getEnrollment: vi.fn().mockRejectedValue(loadError),
      createReEnrollment: vi.fn().mockRejectedValue(submitError)
    })
    failed.reEnrollment.open(releasedEnrollment)
    await expect(failed.reEnrollment.reloadBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    await expect(failed.reEnrollment.submit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(failed.dependencies.onLoadError).toHaveBeenCalledWith(loadError)
    expect(failed.dependencies.onSubmitError).toHaveBeenCalledWith(submitError)

    let resolveCommand
    const pendingSubmit = createReEnrollment({
      createReEnrollment: vi.fn(() => new Promise(resolve => { resolveCommand = resolve }))
    })
    pendingSubmit.reEnrollment.open(releasedEnrollment)
    const submission = pendingSubmit.reEnrollment.submit()
    pendingSubmit.reEnrollment.close()
    resolveCommand({ source_enrollment_version: 11, enrollment: newEnrollment })
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pendingSubmit.dependencies.showCurrentCollection).not.toHaveBeenCalled()

    let resolveReload
    const pendingReload = createReEnrollment({
      getEnrollment: vi.fn(() => new Promise(resolve => { resolveReload = resolve }))
    })
    pendingReload.reEnrollment.open(releasedEnrollment)
    const reload = pendingReload.reEnrollment.reloadBaseline()
    pendingReload.reEnrollment.dispose()
    resolveReload({ ...releasedEnrollment, version: 12 })
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(pendingReload.dependencies.replaceEnrollment).not.toHaveBeenCalled()
    expect(pendingReload.reEnrollment.reloading.value).toBe(false)
  })
})
