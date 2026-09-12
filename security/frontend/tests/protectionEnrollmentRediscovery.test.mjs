import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentRediscovery } from '../src/composables/useProtectionEnrollmentRediscovery.mjs'

const activeEnrollment = {
  id: 'enrollment-1',
  version: 4,
  state: 'active',
  last_discovered_at: '2026-09-12T08:00:00Z',
  latest_source_snapshot_hash: 'snapshot-4'
}

function createRediscovery(overrides = {}) {
  const dependencies = {
    getEnrollment: vi.fn(),
    createDiscoveryExecution: vi.fn().mockResolvedValue({
      execution_id: 'execution-1',
      enrollment_version: 5
    }),
    replaceEnrollment: vi.fn(),
    watchDiscovery: vi.fn(),
    refreshCollection: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    openExecution: vi.fn().mockResolvedValue(),
    onUnavailable: vi.fn(),
    onReloaded: vi.fn(),
    onLoadError: vi.fn(),
    onCreated: vi.fn(),
    onVersionConflict: vi.fn(),
    onExecutionInProgress: vi.fn(),
    onSubmitError: vi.fn(),
    ...overrides
  }
  return {
    dependencies,
    rediscovery: useProtectionEnrollmentRediscovery(dependencies)
  }
}

describe('protection enrollment rediscovery session', () => {
  it('only opens rediscoverable enrollment states', () => {
    const { rediscovery } = createRediscovery()

    expect(rediscovery.open(activeEnrollment)).toBe(true)
    expect(rediscovery.dialog.value).toBe(true)
    expect(rediscovery.enrollment.value).toEqual(activeEnrollment)

    rediscovery.close()
    expect(rediscovery.dialog.value).toBe(false)
    expect(rediscovery.enrollment.value).toBeNull()
    expect(rediscovery.open({ ...activeEnrollment, state: 'released' })).toBe(false)
  })

  it('reloads the authoritative enrollment before reconfirming a conflicted command', async () => {
    const latest = { ...activeEnrollment, version: 6 }
    const { dependencies, rediscovery } = createRediscovery({
      getEnrollment: vi.fn().mockResolvedValue(latest)
    })
    rediscovery.dialogRef.value = { focusPrimary: vi.fn() }
    rediscovery.open(activeEnrollment)
    rediscovery.conflict.value = true

    await expect(rediscovery.reloadBaseline()).resolves.toMatchObject({ status: 'reloaded', enrollment: latest })
    await nextTick()

    expect(dependencies.getEnrollment).toHaveBeenCalledWith('enrollment-1')
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(rediscovery.enrollment.value).toEqual(latest)
    expect(rediscovery.conflict.value).toBe(false)
    expect(dependencies.onReloaded).toHaveBeenCalledOnce()
    expect(rediscovery.dialogRef.value.focusPrimary).toHaveBeenCalledOnce()
  })

  it('closes when the authoritative enrollment can no longer be rediscovered', async () => {
    const latest = { ...activeEnrollment, version: 6, state: 'releasing' }
    const { dependencies, rediscovery } = createRediscovery({
      getEnrollment: vi.fn().mockResolvedValue(latest)
    })
    rediscovery.open(activeEnrollment)

    await expect(rediscovery.reloadBaseline()).resolves.toMatchObject({ status: 'unavailable_state' })

    expect(rediscovery.dialog.value).toBe(false)
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith(latest)
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.onUnavailable).toHaveBeenCalledOnce()
  })

  it('creates one versioned execution, watches its source marker, and opens Monitor', async () => {
    let resolveExecution
    const { dependencies, rediscovery } = createRediscovery({
      createDiscoveryExecution: vi.fn(() => new Promise(resolve => { resolveExecution = resolve }))
    })
    rediscovery.open(activeEnrollment)

    const firstSubmit = rediscovery.submit()
    const secondSubmit = rediscovery.submit()
    resolveExecution({ execution_id: 'execution-8', enrollment_version: 7 })

    await expect(firstSubmit).resolves.toMatchObject({ status: 'created' })
    await expect(secondSubmit).resolves.toEqual({ status: 'unavailable' })
    expect(dependencies.createDiscoveryExecution).toHaveBeenCalledOnce()
    expect(dependencies.createDiscoveryExecution).toHaveBeenCalledWith('enrollment-1', { version: 4 })
    expect(dependencies.replaceEnrollment).toHaveBeenCalledWith({ ...activeEnrollment, version: 7 })
    expect(dependencies.watchDiscovery).toHaveBeenCalledWith(
      'enrollment-1',
      '2026-09-12T08:00:00Z|snapshot-4'
    )
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true, syncFindings: true })
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onCreated).toHaveBeenCalledOnce()
    expect(dependencies.openExecution).toHaveBeenCalledWith('execution-8')
    expect(rediscovery.dialog.value).toBe(false)
  })

  it('preserves the session after a version conflict until an explicit reload', async () => {
    const conflict = { response: { status: 409, data: { error_code: 'resource_version_conflict' } } }
    const { dependencies, rediscovery } = createRediscovery({
      createDiscoveryExecution: vi.fn().mockRejectedValue(conflict)
    })
    rediscovery.open(activeEnrollment)

    await expect(rediscovery.submit()).resolves.toEqual({ status: 'version_conflict', error: conflict })

    expect(rediscovery.dialog.value).toBe(true)
    expect(rediscovery.enrollment.value).toEqual(activeEnrollment)
    expect(rediscovery.conflict.value).toBe(true)
    expect(dependencies.onVersionConflict).toHaveBeenCalledOnce()
    expect(dependencies.watchDiscovery).not.toHaveBeenCalled()
  })

  it('adopts an already-running discovery into the same refresh path', async () => {
    const inProgress = {
      response: { status: 409, data: { error_code: 'protection_discovery_execution_in_progress' } }
    }
    const { dependencies, rediscovery } = createRediscovery({
      createDiscoveryExecution: vi.fn().mockRejectedValue(inProgress)
    })
    rediscovery.open(activeEnrollment)

    await expect(rediscovery.submit()).resolves.toEqual({ status: 'execution_in_progress', error: inProgress })

    expect(dependencies.watchDiscovery).toHaveBeenCalledWith(
      'enrollment-1',
      '2026-09-12T08:00:00Z|snapshot-4'
    )
    expect(dependencies.refreshCollection).toHaveBeenCalledWith({ background: true })
    expect(dependencies.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.onExecutionInProgress).toHaveBeenCalledOnce()
    expect(rediscovery.dialog.value).toBe(false)
  })

  it('reports Monitor navigation failure without treating the accepted command as retryable', async () => {
    const navigationError = new Error('monitor unavailable')
    const { dependencies, rediscovery } = createRediscovery({
      openExecution: vi.fn().mockRejectedValue(navigationError)
    })
    rediscovery.open(activeEnrollment)

    await expect(rediscovery.submit()).resolves.toMatchObject({
      status: 'monitor_open_failed',
      error: navigationError
    })

    expect(dependencies.createDiscoveryExecution).toHaveBeenCalledOnce()
    expect(dependencies.onCreated).toHaveBeenCalledOnce()
    expect(dependencies.onSubmitError).toHaveBeenCalledWith(navigationError)
    expect(rediscovery.dialog.value).toBe(false)
  })

  it('reports actionable failures and invalidates pending work when the session closes or disposes', async () => {
    const loadError = new Error('load failed')
    const submitError = new Error('submit failed')
    const failed = createRediscovery({
      getEnrollment: vi.fn().mockRejectedValue(loadError),
      createDiscoveryExecution: vi.fn().mockRejectedValue(submitError)
    })
    failed.rediscovery.open(activeEnrollment)
    await expect(failed.rediscovery.reloadBaseline()).resolves.toEqual({ status: 'failed', error: loadError })
    await expect(failed.rediscovery.submit()).resolves.toEqual({ status: 'failed', error: submitError })
    expect(failed.dependencies.onLoadError).toHaveBeenCalledWith(loadError)
    expect(failed.dependencies.onSubmitError).toHaveBeenCalledWith(submitError)

    let resolveExecution
    const pendingSubmit = createRediscovery({
      createDiscoveryExecution: vi.fn(() => new Promise(resolve => { resolveExecution = resolve }))
    })
    pendingSubmit.rediscovery.open(activeEnrollment)
    const submission = pendingSubmit.rediscovery.submit()
    pendingSubmit.rediscovery.close()
    resolveExecution({ execution_id: 'stale-execution', enrollment_version: 8 })
    await expect(submission).resolves.toEqual({ status: 'stale' })
    expect(pendingSubmit.dependencies.watchDiscovery).not.toHaveBeenCalled()

    let resolveReload
    const pendingReload = createRediscovery({
      getEnrollment: vi.fn(() => new Promise(resolve => { resolveReload = resolve }))
    })
    pendingReload.rediscovery.open(activeEnrollment)
    const reload = pendingReload.rediscovery.reloadBaseline()
    pendingReload.rediscovery.dispose()
    resolveReload({ ...activeEnrollment, version: 9 })
    await expect(reload).resolves.toEqual({ status: 'stale' })
    expect(pendingReload.dependencies.replaceEnrollment).not.toHaveBeenCalled()
    expect(pendingReload.rediscovery.reloading.value).toBe(false)
  })
})
