import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionExemptionRevocation } from '../src/composables/useProtectionExemptionRevocation.mjs'

const activeExemption = {
  id: 'exemption-1',
  version: 2,
  effective_state: 'active',
  assessment_id: 'assessment-1',
  subject_id: 'requester-1',
  current: { state: 'active', rationale: 'approved' }
}

function createRevocation(overrides = {}) {
  const dependencies = {
    getExemption: vi.fn().mockResolvedValue(activeExemption),
    revokeExemption: vi.fn().mockResolvedValue({ ...activeExemption, version: 3, effective_state: 'revoked' }),
    replaceExemption: vi.fn(),
    refreshExemptions: vi.fn().mockResolvedValue(true),
    refreshCollection: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    t: vi.fn(key => key),
    onReloaded: vi.fn(),
    onAlreadyInactive: vi.fn(),
    onLoadError: vi.fn(),
    onRevoked: vi.fn(),
    onVersionConflict: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    revocation: useProtectionExemptionRevocation(dependencies)
  }
}

function allowValidation(revocation) {
  revocation.dialogRef.value = { validate: vi.fn().mockResolvedValue(true), focusPrimary: vi.fn() }
}

describe('protection exemption revocation session', () => {
  it('opens only an active exemption with required rationale', () => {
    const { revocation } = createRevocation()

    expect(revocation.open(activeExemption)).toBe(true)
    expect(revocation.dialog.value).toBe(true)
    expect(revocation.exemption.value).toEqual(activeExemption)
    expect(revocation.exemption.value).not.toBe(activeExemption)
    expect(revocation.rules.value.rationale[0]).toMatchObject({ required: true, trigger: 'blur', whitespace: true })

    revocation.close()
    expect(revocation.open({ ...activeExemption, effective_state: 'expired' })).toBe(false)
  })

  it('submits one canonical revoke command against the opened version snapshot', async () => {
    let resolveValidation
    const openedExemption = { ...activeExemption, current: { ...activeExemption.current } }
    const { dependencies, revocation } = createRevocation()
    revocation.dialogRef.value = {
      validate: vi.fn(() => new Promise(resolve => { resolveValidation = resolve })),
      focusPrimary: vi.fn()
    }
    revocation.open(openedExemption)
    revocation.form.rationale = ' work completed '
    openedExemption.version = 9
    openedExemption.current.state = 'superseded'

    const firstSubmit = revocation.submit()
    const secondSubmit = revocation.submit()
    resolveValidation(true)

    await expect(firstSubmit).resolves.toEqual({ status: 'revoked' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.revokeExemption).toHaveBeenCalledOnce()
    expect(dependencies.revokeExemption).toHaveBeenCalledWith('exemption-1', {
      version: 2,
      rationale: 'work completed'
    })
    expect(dependencies.refreshExemptions).toHaveBeenCalledOnce()
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onRevoked).toHaveBeenCalledOnce()
    expect(revocation.dialog.value).toBe(false)
  })

  it('keeps the rationale after conflict and adopts only an explicitly reloaded version', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const latest = { ...activeExemption, version: 4, current: { ...activeExemption.current, rationale: 'extended' } }
    const { dependencies, revocation } = createRevocation({
      revokeExemption: vi.fn().mockRejectedValue(conflict),
      getExemption: vi.fn().mockResolvedValue(latest)
    })
    allowValidation(revocation)
    revocation.open(activeExemption)
    revocation.form.rationale = 'preserve revoke basis'

    await expect(revocation.submit()).resolves.toEqual({ status: 'version_conflict', error: conflict })
    expect(revocation.conflict.value).toBe(true)
    expect(revocation.form.rationale).toBe('preserve revoke basis')
    expect(dependencies.onVersionConflict).toHaveBeenCalledOnce()

    await expect(revocation.reloadBaseline()).resolves.toEqual({ status: 'reloaded', exemption: latest })
    await nextTick()
    expect(dependencies.getExemption).toHaveBeenCalledWith('exemption-1')
    expect(dependencies.replaceExemption).toHaveBeenCalledWith(latest)
    expect(revocation.exemption.value.version).toBe(4)
    expect(revocation.form.rationale).toBe('')
    expect(revocation.conflict.value).toBe(false)
    expect(dependencies.onReloaded).toHaveBeenCalledOnce()
    expect(revocation.dialogRef.value.focusPrimary).toHaveBeenCalledOnce()
  })

  it('closes when the authoritative exemption is already inactive', async () => {
    const latest = { ...activeExemption, version: 4, effective_state: 'revoked' }
    const { dependencies, revocation } = createRevocation({ getExemption: vi.fn().mockResolvedValue(latest) })
    revocation.open(activeExemption)

    await expect(revocation.reloadBaseline()).resolves.toEqual({ status: 'already_inactive', exemption: latest })

    expect(dependencies.replaceExemption).toHaveBeenCalledWith(latest)
    expect(revocation.dialog.value).toBe(false)
    expect(dependencies.onAlreadyInactive).toHaveBeenCalledOnce()
  })

  it('keeps invalid and generic failures actionable', async () => {
    const submitError = new Error('submit failed')
    const { dependencies, revocation } = createRevocation({ revokeExemption: vi.fn().mockRejectedValue(submitError) })
    revocation.dialogRef.value = { validate: vi.fn().mockResolvedValue(false) }
    revocation.open(activeExemption)

    await expect(revocation.submit()).resolves.toEqual({ status: 'invalid' })
    expect(dependencies.revokeExemption).not.toHaveBeenCalled()

    allowValidation(revocation)
    revocation.form.rationale = 'valid rationale'
    await expect(revocation.submit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(revocation.dialog.value).toBe(true)
    expect(dependencies.onSubmitError).toHaveBeenCalledWith(submitError)
  })

  it('reports reload failures without discarding the current revoke basis', async () => {
    const loadError = new Error('load failed')
    const { dependencies, revocation } = createRevocation({ getExemption: vi.fn().mockRejectedValue(loadError) })
    revocation.open(activeExemption)
    revocation.form.rationale = 'keep current basis'

    await expect(revocation.reloadBaseline()).resolves.toEqual({ status: 'failed', error: loadError })

    expect(revocation.dialog.value).toBe(true)
    expect(revocation.form.rationale).toBe('keep current basis')
    expect(dependencies.onLoadError).toHaveBeenCalledWith(loadError)
  })

  it('invalidates pending reloads and submits when the session closes or is disposed', async () => {
    let resolveSubmit
    const pendingSubmit = createRevocation({
      revokeExemption: vi.fn(() => new Promise(resolve => { resolveSubmit = resolve }))
    })
    allowValidation(pendingSubmit.revocation)
    pendingSubmit.revocation.open(activeExemption)
    const submission = pendingSubmit.revocation.submit()
    await vi.waitFor(() => expect(resolveSubmit).toBeTypeOf('function'))
    pendingSubmit.revocation.close()
    resolveSubmit({})
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pendingSubmit.dependencies.refreshExemptions).not.toHaveBeenCalled()

    let resolveReload
    const pendingReload = createRevocation({
      getExemption: vi.fn(() => new Promise(resolve => { resolveReload = resolve }))
    })
    pendingReload.revocation.open(activeExemption)
    const reload = pendingReload.revocation.reloadBaseline()
    pendingReload.revocation.dispose()
    resolveReload({ ...activeExemption, version: 5 })
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(pendingReload.dependencies.replaceExemption).not.toHaveBeenCalled()
    expect(pendingReload.revocation.reloading.value).toBe(false)
  })
})
