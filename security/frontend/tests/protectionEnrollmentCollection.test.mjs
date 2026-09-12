import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentCollection } from '../src/composables/useProtectionEnrollmentCollection.mjs'

function createRuntime(initialNow = 1000) {
  let now = initialNow
  let hidden = false
  let visibilityListener = null
  let nextTimerID = 1
  const timers = new Map()

  return {
    runtime: {
      now: () => now,
      setTimeout: (callback, delay) => {
        const id = nextTimerID++
        timers.set(id, { callback, delay })
        return id
      },
      clearTimeout: id => timers.delete(id),
      isHidden: () => hidden,
      addVisibilityListener: callback => { visibilityListener = callback },
      removeVisibilityListener: callback => {
        if (visibilityListener === callback) visibilityListener = null
      }
    },
    advanceTo(value) { now = value },
    setHidden(value) { hidden = value },
    visibilityListener: () => visibilityListener,
    async runNextTimer() {
      const [id, timer] = timers.entries().next().value
      timers.delete(id)
      await timer.callback()
    },
    timers
  }
}

describe('protected resource collection', () => {
  it('owns list parameters, result state, refresh time, and request serialization', async () => {
    const harness = createRuntime()
    let resolveList
    const listEnrollments = vi.fn(() => new Promise(resolve => { resolveList = resolve }))
    const onRowsLoaded = vi.fn()
    const collection = useProtectionEnrollmentCollection({ listEnrollments, onRowsLoaded, runtime: harness.runtime })
    collection.listScope.value = 'released'
    collection.currentPage.value = 3
    collection.pageSize.value = 50

    const firstLoad = collection.load()
    expect(collection.loading.value).toBe(true)
    await expect(collection.load({ background: true })).resolves.toBe(false)

    resolveList({ data: [{ id: 7, state: 'released' }], total: 61 })
    await expect(firstLoad).resolves.toBe(true)

    expect(listEnrollments).toHaveBeenCalledWith({ scope: 'released', page: 3, page_size: 50 })
    expect(collection.rows.value).toEqual([{ id: 7, state: 'released' }])
    expect(collection.total.value).toBe(61)
    expect(collection.lastRefreshedAt.value).toEqual(new Date(1000))
    expect(collection.loading.value).toBe(false)
    expect(onRowsLoaded).toHaveBeenCalledWith({ rows: collection.rows.value, options: {} })
  })

  it('uses fast then slow polling and stops once the refresh window times out', async () => {
    const harness = createRuntime()
    const onAutoRefreshTimeout = vi.fn()
    const listEnrollments = vi.fn().mockResolvedValue({
      data: [{ id: 1, state: 'active', owner_progress: [{ acknowledged: false }] }],
      total: 1
    })
    const collection = useProtectionEnrollmentCollection({ listEnrollments, onAutoRefreshTimeout, runtime: harness.runtime })
    collection.setCollection([{ id: 1, state: 'active', owner_progress: [{ acknowledged: false }] }], 1)

    collection.scheduleAutoRefresh({ reset: true })
    expect(collection.autoRefreshActive.value).toBe(true)
    expect([...harness.timers.values()].map(timer => timer.delay)).toEqual([2000])

    harness.advanceTo(32000)
    await harness.runNextTimer()
    expect([...harness.timers.values()].map(timer => timer.delay)).toEqual([5000])

    harness.advanceTo(121001)
    await harness.runNextTimer()
    expect(collection.autoRefreshActive.value).toBe(false)
    expect(harness.timers.size).toBe(0)
    expect(onAutoRefreshTimeout).toHaveBeenCalledTimes(1)
  })

  it('pauses polling while hidden and owns visibility listener cleanup', async () => {
    const harness = createRuntime()
    const listEnrollments = vi.fn().mockResolvedValue({ data: [], total: 0 })
    const collection = useProtectionEnrollmentCollection({ listEnrollments, runtime: harness.runtime })
    collection.setCollection([{ id: 1, state: 'active', owner_progress: [{ acknowledged: false }] }], 1)
    collection.startVisibilityTracking()
    collection.scheduleAutoRefresh({ reset: true })

    harness.setHidden(true)
    harness.visibilityListener()()
    expect(harness.timers.size).toBe(0)

    harness.advanceTo(61000)
    harness.setHidden(false)
    harness.visibilityListener()()
    await Promise.resolve()
    await Promise.resolve()
    expect(listEnrollments).toHaveBeenCalledTimes(1)

    collection.dispose()
    expect(collection.autoRefreshActive.value).toBe(false)
    expect(harness.visibilityListener()).toBeNull()
  })

  it('does not recreate a timer when an in-flight refresh finishes after disposal', async () => {
    const harness = createRuntime()
    let resolveList
    const listEnrollments = vi.fn(() => new Promise(resolve => { resolveList = resolve }))
    const collection = useProtectionEnrollmentCollection({ listEnrollments, runtime: harness.runtime })
    collection.setCollection([{ id: 1, state: 'active', owner_progress: [{ acknowledged: false }] }], 1)
    collection.scheduleAutoRefresh({ reset: true })

    const refresh = harness.runNextTimer()
    expect(collection.backgroundRefreshing.value).toBe(true)
    collection.dispose()
    resolveList({ data: [{ id: 1, state: 'active', owner_progress: [{ acknowledged: false }] }], total: 1 })
    await refresh

    expect(collection.autoRefreshActive.value).toBe(false)
    expect(harness.timers.size).toBe(0)
  })

  it('keeps explicit discovery polling only until the discovery marker changes', () => {
    const harness = createRuntime()
    const collection = useProtectionEnrollmentCollection({
      listEnrollments: vi.fn(),
      runtime: harness.runtime
    })
    collection.setCollection([{ id: 1, state: 'active', last_discovered_at: '2026-09-12T00:00:00Z', latest_source_snapshot_hash: 'old', owner_progress: [] }], 1)
    collection.watchDiscovery(1, '2026-09-12T00:00:00Z|old')
    collection.scheduleAutoRefresh({ reset: true })
    expect(collection.autoRefreshActive.value).toBe(true)

    collection.setCollection([{ id: 1, state: 'active', last_discovered_at: '2026-09-12T00:01:00Z', latest_source_snapshot_hash: 'new', owner_progress: [] }], 1)
    collection.scheduleAutoRefresh()
    expect(collection.autoRefreshActive.value).toBe(false)
    expect(harness.timers.size).toBe(0)
  })
})
