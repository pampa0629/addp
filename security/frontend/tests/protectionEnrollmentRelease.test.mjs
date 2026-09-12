import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentRelease } from '../src/composables/useProtectionEnrollmentRelease.mjs'

const activeEnrollment = {
  id: 'enrollment-1',
  version: 4,
  state: 'active',
  discovery_summary: { status: 'completed', finding_count: 0 }
}

function createRelease(overrides = {}) {
  const dependencies = {
    getEnrollment: vi.fn(),
    releaseEnrollment: vi.fn().mockResolvedValue({ ...activeEnrollment, version: 5, state: 'releasing' }),
    replaceEnrollment: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    t: vi.fn(key => key),
    onAlreadyReleasing: vi.fn(),
    onNoSupportedFindingsUnavailable: vi.fn(),
    onReloaded: vi.fn(),
    onLoadError: vi.fn(),
    onStarted: vi.fn(),
    onVersionConflict: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return { dependencies, release: useProtectionEnrollmentRelease(dependencies) }
}

function allowValidation(release) {
  release.formRef.value = { validate: vi.fn().mockResolvedValue(true), clearValidate: vi.fn() }
}

describe('protection enrollment release session', () => {
  it('opens the single release flow with basis-specific presentation and required reason', () => {
    const { release } = createRelease()

    expect(release.open(activeEnrollment, 'no_supported_findings')).toBe(true)
    expect(release.dialog.value).toBe(true)
    expect(release.enrollment.value).toEqual(activeEnrollment)
    expect(release.basis.value).toBe('no_supported_findings')
    expect(release.dialogTitle.value).toBe('security.enrollment.confirmNoProtectionNeeded')
    expect(release.dialogWarning.value).toBe('security.enrollment.noFindingsReleaseWarning')
    expect(release.reasonPlaceholder.value).toBe('security.enrollment.noFindingsReleaseReason')
    expect(release.confirmLabel.value).toBe('security.enrollment.confirmNoProtectionNeededAction')
    expect(release.rules.value.reason[0]).toMatchObject({ required: true, trigger: 'blur', whitespace: true })
    expect(release.open({ ...activeEnrollment, state: 'released' })).toBe(false)
  })

  it('submits one canonical release command while async validation is pending', async () => {
    let resolveValidation
    const { dependencies, release } = createRelease()
    release.formRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      clearValidate: vi.fn()
    }
    release.open(activeEnrollment, 'manual')
    release.form.reason = ' business owner confirmed '

    const firstSubmit = release.submit()
    const secondSubmit = release.submit()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toMatchObject({ status: 'started' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.releaseEnrollment).toHaveBeenCalledOnce()
    expect(dependencies.releaseEnrollment).toHaveBeenCalledWith('enrollment-1', {
      version: 4,
      basis: 'manual',
      reason: 'business owner confirmed'
    })
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(expect.objectContaining({ state: 'releasing' }))
    expect(dependencies.refreshCollection).toHaveBeenCalledWith()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onStarted).toHaveBeenCalledOnce()
    expect(release.dialog.value).toBe(false)
  })

  it('preserves the reason on conflict and reloads the authoritative version before retry', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latest = { ...activeEnrollment, version: 6 }
    const { dependencies, release } = createRelease({
      releaseEnrollment: vi.fn().mockRejectedValue(conflict),
      getEnrollment: vi.fn().mockResolvedValue(latest)
    })
    allowValidation(release)
    release.cancelButton.value = { focus: vi.fn() }
    release.open(activeEnrollment, 'manual')
    release.form.reason = 'keep this until reload'

    await expect(release.submit()).resolves.toMatchObject({ status: 'version_conflict' })
    expect(release.conflict.value).toBe(true)
    expect(release.form.reason).toBe('keep this until reload')
    expect(dependencies.onVersionConflict).toHaveBeenCalledOnce()

    await expect(release.reloadBaseline()).resolves.toMatchObject({ status: 'reloaded', enrollment: latest })
    await nextTick()
    expect(dependencies.getEnrollment).toHaveBeenCalledWith('enrollment-1')
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(release.enrollment.value).toEqual(latest)
    expect(release.form.reason).toBe('')
    expect(release.conflict.value).toBe(false)
    expect(dependencies.onReloaded).toHaveBeenCalledOnce()
    expect(release.cancelButton.value.focus).toHaveBeenCalled()
  })

  it('closes when the authoritative enrollment is already releasing', async () => {
    const latest = { ...activeEnrollment, version: 6, state: 'releasing' }
    const { dependencies, release } = createRelease({ getEnrollment: vi.fn().mockResolvedValue(latest) })
    release.open(activeEnrollment)

    await expect(release.reloadBaseline()).resolves.toMatchObject({ status: 'already_releasing' })

    expect(release.dialog.value).toBe(false)
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onAlreadyReleasing).toHaveBeenCalledOnce()
  })

  it('closes no-supported-findings release when the authoritative evidence changed', async () => {
    const latest = {
      ...activeEnrollment,
      version: 6,
      discovery_summary: { status: 'completed', finding_count: 1 }
    }
    const { dependencies, release } = createRelease({ getEnrollment: vi.fn().mockResolvedValue(latest) })
    release.open(activeEnrollment, 'no_supported_findings')

    await expect(release.reloadBaseline()).resolves.toMatchObject({ status: 'no_supported_findings_unavailable' })

    expect(release.dialog.value).toBe(false)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onNoSupportedFindingsUnavailable).toHaveBeenCalledOnce()
  })

  it('handles a server rejection of stale no-supported-findings evidence', async () => {
    const unavailable = { response: { status: 409, data: { error_code: 'no_supported_findings_release_unavailable' } } }
    const { dependencies, release } = createRelease({ releaseEnrollment: vi.fn().mockRejectedValue(unavailable) })
    allowValidation(release)
    release.open(activeEnrollment, 'no_supported_findings')
    release.form.reason = 'current scan found nothing'

    await expect(release.submit()).resolves.toMatchObject({ status: 'no_supported_findings_unavailable' })

    expect(release.dialog.value).toBe(false)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onNoSupportedFindingsUnavailable).toHaveBeenCalledOnce()
    expect(dependencies.onSubmitError).not.toHaveBeenCalled()
  })

  it('keeps generic failures actionable and invalidates an in-flight reload on disposal', async () => {
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const failed = createRelease({
      getEnrollment: vi.fn().mockRejectedValue(loadError),
      releaseEnrollment: vi.fn().mockRejectedValue(submitError)
    })
    allowValidation(failed.release)
    failed.release.open(activeEnrollment)
    await expect(failed.release.reloadBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    expect(failed.dependencies.onLoadError).toHaveBeenCalledWith(loadError)
    await expect(failed.release.submit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(failed.release.dialog.value).toBe(true)
    expect(failed.dependencies.onSubmitError).toHaveBeenCalledWith(submitError)

    let resolveReload
    const pending = createRelease({
      getEnrollment: vi.fn(() => new Promise(resolve => { resolveReload = resolve }))
    })
    pending.release.open(activeEnrollment)
    const reload = pending.release.reloadBaseline()
    pending.release.dispose()
    resolveReload({ ...activeEnrollment, version: 7 })
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(pending.dependencies.replaceEnrollment).not.toHaveBeenCalled()
    expect(pending.release.reloading.value).toBe(false)
  })
})
