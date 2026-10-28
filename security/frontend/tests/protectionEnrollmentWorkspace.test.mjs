import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import {
  resolveProtectionEnrollmentWorkspaceRouteState,
  useProtectionEnrollmentWorkspace
} from '../src/composables/useProtectionEnrollmentWorkspace.mjs'

function deferred() {
  let resolve
  const promise = new Promise(resolvePromise => { resolve = resolvePromise })
  return { promise, resolve }
}

function createWorkspace(overrides = {}) {
  const route = { query: {}, fullPath: '/protection-enrollments' }
  const router = {
    resolve: vi.fn(location => ({ fullPath: `/resolved/${JSON.stringify(location)}` })),
    push: vi.fn().mockResolvedValue(undefined),
    replace: vi.fn().mockResolvedValue(undefined)
  }
  const activeWorkspace = ref('resources')
  const reviewQueue = {
    page: ref(1),
    applyRouteState: vi.fn(),
    routeQuery: vi.fn(() => ({ tab: 'review-queue', page: '1', page_size: '20' })),
    load: vi.fn().mockResolvedValue(true),
    prepareFilterChange: vi.fn(),
    resetFilters: vi.fn()
  }
  const resources = {
    findingsPage: ref(1),
    openLocator: vi.fn().mockResolvedValue({ status: 'opened' }),
    onNotEnrolled: vi.fn(),
    load: vi.fn().mockResolvedValue(true),
    loadAccessRequests: vi.fn().mockResolvedValue(true),
    loadGovernance: vi.fn().mockResolvedValue(true),
    scheduleAutoRefresh: vi.fn(),
    stopAutoRefresh: vi.fn(),
    startVisibilityTracking: vi.fn()
  }
  const references = {
    loadDefinitions: vi.fn().mockResolvedValue(true),
    loadDetectorCapabilities: vi.fn().mockResolvedValue(true),
    loadEngines: vi.fn().mockResolvedValue(true)
  }
  const creation = {
    canCreate: ref(true),
    open: vi.fn()
  }
  const dependencies = {
    route,
    router,
    activeWorkspace,
    reviewQueue,
    resources,
    references,
    creation,
    ...overrides
  }
  return {
    dependencies,
    workspace: useProtectionEnrollmentWorkspace(dependencies)
  }
}

describe('protected-resource workspace lifecycle', () => {
  it('canonicalizes workspace routes while preserving only their owned query', () => {
    expect(resolveProtectionEnrollmentWorkspaceRouteState({ action: 'enroll', locator: 'engine://7' })).toMatchObject({
      tab: 'resources',
      query: { locator: 'engine://7' },
      changed: true
    })
    expect(resolveProtectionEnrollmentWorkspaceRouteState({ tab: 'review-queue', page: '2', page_size: '50', action: 'enroll' })).toMatchObject({
      tab: 'review-queue',
      query: { tab: 'review-queue', page: '2', page_size: '50' }
    })
  })

  it('applies a route before mount without starting workspace loads', async () => {
    const { dependencies, workspace } = createWorkspace()

    await expect(workspace.handleRouteChange({ locator: 'engine://7' })).resolves.toBe(true)

    expect(dependencies.reviewQueue.applyRouteState).toHaveBeenCalledOnce()
    expect(dependencies.creation.open).not.toHaveBeenCalled()
    expect(dependencies.resources.openLocator).not.toHaveBeenCalled()
    expect(dependencies.resources.load).not.toHaveBeenCalled()
    expect(dependencies.reviewQueue.load).not.toHaveBeenCalled()
  })

  it('mounts the current resource workspace and starts refresh only after loading', async () => {
    const { dependencies, workspace } = createWorkspace()
    await workspace.handleRouteChange({})

    await expect(workspace.mount()).resolves.toBe(true)

    expect(dependencies.resources.startVisibilityTracking).toHaveBeenCalledOnce()
    expect(dependencies.resources.load).toHaveBeenCalledOnce()
    expect(dependencies.resources.loadAccessRequests).toHaveBeenCalledOnce()
    expect(dependencies.resources.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })
    expect(dependencies.references.loadEngines).toHaveBeenCalledOnce()
  })

  it('does not start resource auto-refresh after a newer review-queue transition', async () => {
    const resourceLoad = deferred()
    const resources = {
      findingsPage: ref(1),
      load: vi.fn(() => resourceLoad.promise),
      loadAccessRequests: vi.fn().mockResolvedValue(true),
      loadGovernance: vi.fn().mockResolvedValue(true),
      scheduleAutoRefresh: vi.fn(),
      stopAutoRefresh: vi.fn(),
      startVisibilityTracking: vi.fn()
    }
    const { dependencies, workspace } = createWorkspace({ resources })
    await workspace.handleRouteChange({})
    const mounting = workspace.mount()

    dependencies.route.query = { tab: 'review-queue' }
    await expect(workspace.handleRouteChange(dependencies.route.query)).resolves.toBe(true)
    resourceLoad.resolve(true)
    await expect(mounting).resolves.toBe(false)

    expect(dependencies.reviewQueue.load).toHaveBeenCalledWith(1)
    expect(resources.stopAutoRefresh).toHaveBeenCalled()
    expect(resources.scheduleAutoRefresh).not.toHaveBeenCalled()
  })

  it('invalidates current loading as soon as an explicit workspace navigation starts', async () => {
    const resourceLoad = deferred()
    const resources = {
      findingsPage: ref(1),
      load: vi.fn(() => resourceLoad.promise),
      loadAccessRequests: vi.fn().mockResolvedValue(true),
      loadGovernance: vi.fn().mockResolvedValue(true),
      scheduleAutoRefresh: vi.fn(),
      stopAutoRefresh: vi.fn(),
      startVisibilityTracking: vi.fn()
    }
    const { dependencies, workspace } = createWorkspace({ resources })
    await workspace.handleRouteChange({})
    const mounting = workspace.mount()

    dependencies.activeWorkspace.value = 'review-queue'
    await expect(workspace.handleWorkspaceChange('review-queue')).resolves.toBe(true)
    resourceLoad.resolve(true)
    await expect(mounting).resolves.toBe(false)

    expect(dependencies.router.replace).toHaveBeenCalledWith({
      path: '/protection-enrollments',
      query: { tab: 'review-queue', page: '1', page_size: '20' }
    })
    expect(resources.scheduleAutoRefresh).not.toHaveBeenCalled()
  })

  it('does not rewrite the route from a stale review-queue page correction', async () => {
    const reviewLoad = deferred()
    const { dependencies, workspace } = createWorkspace()
    dependencies.reviewQueue.load = vi.fn(() => reviewLoad.promise)
    dependencies.route.query = { tab: 'review-queue', page: '9' }
    await workspace.handleRouteChange(dependencies.route.query)
    const mounting = workspace.mount()

    dependencies.route.query = {}
    await expect(workspace.handleRouteChange({})).resolves.toBe(true)
    dependencies.reviewQueue.page.value = 2
    reviewLoad.resolve(true)
    await expect(mounting).resolves.toBe(false)

    expect(dependencies.router.resolve).not.toHaveBeenCalled()
  })

  it('refreshes only the current resource workspace and invalidates work on disposal', async () => {
    const { dependencies, workspace } = createWorkspace()
    await workspace.handleRouteChange({})
    await workspace.mount()
    dependencies.resources.scheduleAutoRefresh.mockClear()

    await expect(workspace.manualRefresh()).resolves.toBe(true)
    expect(dependencies.resources.load).toHaveBeenCalledWith({ background: true })
    expect(dependencies.resources.loadGovernance).toHaveBeenCalledWith(1)
    expect(dependencies.resources.scheduleAutoRefresh).toHaveBeenCalledWith({ reset: true })

    workspace.dispose()
    await expect(workspace.handleRouteChange({ tab: 'review-queue' })).resolves.toBe(false)
    await expect(workspace.manualRefresh()).resolves.toBe(false)
    expect(workspace.manualRefreshing.value).toBe(false)
  })
})

describe('resource deep link opens detail before offering enrollment', () => {
  it('opens existing protection without creation permission', async () => {
    const { workspace, dependencies } = createWorkspace()
    dependencies.creation.canCreate.value = false
    await workspace.handleRouteChange({ locator: 'resource-locator' })
    await workspace.mount()
    expect(dependencies.resources.openLocator).toHaveBeenCalledWith('resource-locator', { isCurrent: expect.any(Function) })
    expect(dependencies.creation.open).not.toHaveBeenCalled()
    expect(dependencies.resources.onNotEnrolled).not.toHaveBeenCalled()
  })
  it.each([true, false])('handles confirmed absence with creation permission %s', async canCreate => {
    const { workspace, dependencies } = createWorkspace()
    dependencies.creation.canCreate.value = canCreate
    dependencies.resources.openLocator.mockResolvedValue({ status: 'not_enrolled' })
    await workspace.handleRouteChange({ locator: 'resource-locator' })
    await workspace.mount()
    expect(dependencies.creation.open).toHaveBeenCalledTimes(canCreate ? 1 : 0)
    expect(dependencies.resources.onNotEnrolled).toHaveBeenCalledTimes(canCreate ? 0 : 1)
  })
  it('does not open creation after a failed lookup', async () => {
    const { workspace, dependencies } = createWorkspace()
    dependencies.resources.openLocator.mockResolvedValue({ status: 'failed' })
    await workspace.handleRouteChange({ locator: 'resource-locator' })
    await workspace.mount()
    expect(dependencies.creation.open).not.toHaveBeenCalled()
  })
  it.each(['route', 'dispose'])('does not create after a stale %s lookup', async kind => {
    const pending = deferred()
    const { workspace, dependencies } = createWorkspace()
    dependencies.resources.openLocator.mockReturnValue(pending.promise)
    await workspace.handleRouteChange({ locator: 'resource-locator' })
    const mounting = workspace.mount()
    await vi.waitFor(() => expect(dependencies.resources.openLocator).toHaveBeenCalledOnce())
    if (kind === 'route') await workspace.handleRouteChange({ tab: 'review-queue' })
    else workspace.dispose()
    pending.resolve({ status: 'not_enrolled' })
    await expect(mounting).resolves.toBe(false)
    expect(dependencies.creation.open).not.toHaveBeenCalled()
  })
})
