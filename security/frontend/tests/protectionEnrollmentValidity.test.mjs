import { describe, expect, it, vi } from 'vitest'
import { protectionValidityPayload, useProtectionEnrollmentValidity } from '../src/composables/useProtectionEnrollmentValidity.mjs'

const row = { id: 'resource-1', state: 'active', version: 3, validity_mode: 'until', expires_at: '2099-01-01T00:00:00Z' }
function session(overrides = {}) {
  const dependencies = { getEnrollment: vi.fn().mockResolvedValue({ ...row, version: 4 }), updateValidity: vi.fn().mockResolvedValue({ ...row, version: 4 }), replaceEnrollment: vi.fn(), onSaved: vi.fn(), onInvalid: vi.fn(), onError: vi.fn(), scheduleAutoRefresh: vi.fn(), ...overrides }
  return { dependencies, state: useProtectionEnrollmentValidity(dependencies) }
}
describe('resource protection validity', () => {
  it('uses an explicit null for permanent rules and validates deadlines', () => {
    expect(protectionValidityPayload({ validity_mode: 'permanent', expires_at: 'stale input' })).toEqual({ validity_mode: 'permanent', expires_at: null })
    for (const date of [null, '', 'invalid', new Date(0)]) expect(protectionValidityPayload({ validity_mode: 'until', expires_at: date })).toBeNull()
    expect(protectionValidityPayload({ validity_mode: 'until', expires_at: new Date(row.expires_at) }).expires_at).toBe('2099-01-01T00:00:00.000Z')
  })
  it('preserves a selected deadline on conflict, blocks retry and reloads only on demand', async () => {
    const { state, dependencies } = session({ updateValidity: vi.fn().mockRejectedValue({ response: { status: 409, data: { error_code: 'resource_version_conflict' } } }) })
    state.open(row)
    const selected = new Date('2098-01-01T00:00:00Z')
    state.form.expires_at = selected
    await state.submit()
    expect(state.dialog.value).toBe(true)
    expect(state.conflict.value).toBe(true)
    expect(state.form.expires_at).toBe(selected)
    await state.submit()
    expect(dependencies.updateValidity).toHaveBeenCalledOnce()
    expect(dependencies.getEnrollment).not.toHaveBeenCalled()
    await state.reload()
    expect(state.conflict.value).toBe(false)
    expect(state.enrollment.value.version).toBe(4)
    expect(state.form.expires_at.toISOString()).toBe('2099-01-01T00:00:00.000Z')
  })
  it('saves against the captured version and updates projections without another discovery command', async () => {
    const { state, dependencies } = session()
    state.open(row)
    state.form.validity_mode = 'permanent'
    await state.submit()
    expect(dependencies.updateValidity).toHaveBeenCalledWith(row.id, { version: 3, validity_mode: 'permanent', expires_at: null })
    expect(dependencies.replaceEnrollment).toHaveBeenCalledOnce()
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(state.dialog.value).toBe(false)
    expect(state.open({ ...row, state: 'released' })).toBe(false)
  })
  it('ignores late responses from an unmounted workspace', async () => {
    let resolve
    const { state, dependencies } = session({ updateValidity: () => new Promise(done => { resolve = done }) })
    state.open(row)
    state.form.validity_mode = 'permanent'
    const pending = state.submit()
    state.dispose()
    resolve(row)
    await pending
    expect(dependencies.replaceEnrollment).not.toHaveBeenCalled()
    expect(dependencies.onSaved).not.toHaveBeenCalled()
  })
})
