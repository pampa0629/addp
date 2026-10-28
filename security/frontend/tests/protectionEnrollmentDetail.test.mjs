import { nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentDetail } from '../src/composables/useProtectionEnrollmentDetail.mjs'

const firstEnrollment = {
  id: 7,
  last_discovered_at: '2026-09-12T01:00:00Z',
  latest_source_snapshot_hash: 'snapshot-a',
  latest_discovery_execution_id: 11
}

const secondEnrollment = {
  id: 8,
  last_discovered_at: '2026-09-12T02:00:00Z',
  latest_source_snapshot_hash: 'snapshot-b',
  latest_discovery_execution_id: 12
}

function createDetail(overrides = {}) {
  const enrollment = overrides.enrollment || ref(null)
  const dependencies = {
    enrollment,
    getEnrollment: vi.fn().mockResolvedValue(firstEnrollment),
    loadGovernance: vi.fn().mockResolvedValue(true),
    resetGovernance: vi.fn(),
    loadDefinitions: vi.fn().mockResolvedValue(true),
    navigateToResources: vi.fn().mockResolvedValue(true),
    listPendingFindings: vi.fn().mockResolvedValue({ data: [{ id: 51 }] }),
    onAuthorizationLoadError: vi.fn(),
    onResourceLoadError: vi.fn(),
    onDefinitionsLoadError: vi.fn(),
    ...overrides
  }
  return {
    enrollment,
    dependencies,
    detail: useProtectionEnrollmentDetail(dependencies)
  }
}

describe('protected resource detail session', () => {
  it('opens a resource, loads its governance context, and focuses a requested exemption', async () => {
    const { dependencies, detail, enrollment } = createDetail()
    detail.drawerRef.value = { focusExemption: vi.fn().mockResolvedValue(true) }

    await expect(detail.open(firstEnrollment, 'exemption-9')).resolves.toEqual({ status: 'opened', enrollment: firstEnrollment })
    await nextTick()

    expect(enrollment.value).toEqual(firstEnrollment)
    expect(detail.drawer.value).toBe(true)
    expect(detail.focusedExemptionID.value).toBe('exemption-9')
    expect(dependencies.resetGovernance).toHaveBeenCalledOnce()
    expect(dependencies.loadGovernance).toHaveBeenCalledWith(1)
    expect(dependencies.loadDefinitions).toHaveBeenCalledOnce()
    expect(detail.drawerRef.value.focusExemption).toHaveBeenCalledOnce()
  })

  it('opens an authorization from its authoritative enrollment and reports load errors', async () => {
    const loadError = new Error('authorization enrollment load failed')
    const { dependencies, detail } = createDetail({
      getEnrollment: vi.fn()
        .mockResolvedValueOnce(firstEnrollment)
        .mockRejectedValueOnce(loadError)
    })

    await expect(detail.openAuthorization({ enrollment_id: 7, exemption_id: 'exemption-3' })).resolves.toMatchObject({ status: 'opened' })
    expect(dependencies.getEnrollment).toHaveBeenNthCalledWith(1, 7)
    expect(detail.focusedExemptionID.value).toBe('exemption-3')

    await expect(detail.openAuthorization({ enrollment_id: 8 })).resolves.toEqual({ status: 'failed', error: loadError })
    expect(dependencies.onAuthorizationLoadError).toHaveBeenCalledWith(loadError)
  })

  it('navigates review-queue findings before opening and rejects a slower previous fetch', async () => {
    const pending = new Map()
    const getEnrollment = vi.fn(id => new Promise(resolve => pending.set(id, resolve)))
    const { dependencies, detail } = createDetail({ getEnrollment })

    const firstOpen = detail.openFinding({ enrollment_id: 7 })
    const secondOpen = detail.openFinding({ enrollment_id: 8 })
    pending.get(8)(secondEnrollment)
    await expect(secondOpen).resolves.toEqual({ status: 'opened', enrollment: secondEnrollment })
    pending.get(7)(firstEnrollment)
    await expect(firstOpen).resolves.toEqual({ status: 'stale' })

    expect(dependencies.navigateToResources).toHaveBeenCalledOnce()
    expect(detail.enrollment.value).toEqual(secondEnrollment)
  })

  it('invalidates a pending detail load when the drawer closes', async () => {
    let resolveGovernance
    const loadGovernance = vi.fn(() => new Promise(resolve => { resolveGovernance = resolve }))
    const { dependencies, detail, enrollment } = createDetail({ loadGovernance })
    detail.drawerRef.value = { focusExemption: vi.fn() }

    const opening = detail.open(firstEnrollment, 'exemption-4')
    expect(detail.requestClose()).toBe(true)
    resolveGovernance(true)
    await expect(opening).resolves.toEqual({ status: 'stale' })
    expect(detail.drawerRef.value.focusExemption).not.toHaveBeenCalled()

    detail.closed()
    expect(enrollment.value).toBeNull()
    expect(detail.focusedExemptionID.value).toBe('')
    expect(dependencies.resetGovernance).toHaveBeenCalledTimes(2)
  })

  it('synchronizes the open detail and reloads governance only when discovery changes', async () => {
    const enrollment = ref(firstEnrollment)
    const { dependencies, detail } = createDetail({ enrollment })
    detail.drawer.value = true
    const refreshed = { ...firstEnrollment, last_discovered_at: '2026-09-12T01:01:00Z', latest_source_snapshot_hash: 'snapshot-c' }

    await expect(detail.syncLoadedRows({ rows: [refreshed], options: { syncFindings: true } })).resolves.toBe(true)

    expect(enrollment.value).toEqual(refreshed)
    expect(dependencies.getEnrollment).not.toHaveBeenCalled()
    expect(dependencies.loadGovernance).toHaveBeenCalledWith(1)
    expect(dependencies.loadDefinitions).toHaveBeenCalledOnce()

    dependencies.loadGovernance.mockClear()
    dependencies.loadDefinitions.mockClear()
    await expect(detail.syncLoadedRows({ rows: [refreshed], options: { syncFindings: true } })).resolves.toBe(true)
    expect(dependencies.loadGovernance).not.toHaveBeenCalled()
    expect(dependencies.loadDefinitions).not.toHaveBeenCalled()
  })

  it('uses an authoritative fallback when the open detail is outside the visible page', async () => {
    const enrollment = ref(firstEnrollment)
    const { dependencies, detail } = createDetail({
      enrollment,
      getEnrollment: vi.fn().mockResolvedValue({ ...firstEnrollment, version: 4 })
    })
    detail.drawer.value = true

    await expect(detail.syncLoadedRows({ rows: [secondEnrollment], options: {} })).resolves.toBe(true)

    expect(dependencies.getEnrollment).toHaveBeenCalledWith(7)
    expect(enrollment.value.version).toBe(4)
  })

  it('loads the next pending finding from the current discovery snapshot and rejects stale results', async () => {
    let resolveFindings
    const listPendingFindings = vi.fn(() => new Promise(resolve => { resolveFindings = resolve }))
    const { dependencies, detail, enrollment } = createDetail({ enrollment: ref(firstEnrollment), listPendingFindings })
    detail.drawer.value = true

    const pendingFinding = detail.nextPendingFinding()
    expect(dependencies.listPendingFindings).toHaveBeenCalledWith({
      enrollment_id: 7,
      source_snapshot_hash: 'snapshot-a',
      discovery_execution_id: 11,
      review_state: 'pending',
      page: 1,
      page_size: 1
    })
    detail.requestClose()
    resolveFindings({ data: [{ id: 51 }] })
    await expect(pendingFinding).resolves.toBeNull()

    enrollment.value = firstEnrollment
    detail.drawer.value = true
    dependencies.listPendingFindings.mockResolvedValueOnce({ data: [{ id: 52 }] })
    await expect(detail.nextPendingFinding()).resolves.toEqual({ id: 52 })
  })

  it('replaces only the current enrollment and clears the session on disposal', () => {
    const { dependencies, detail, enrollment } = createDetail({ enrollment: ref(firstEnrollment) })
    detail.drawer.value = true

    expect(detail.replace({ ...secondEnrollment, version: 2 })).toBe(false)
    expect(enrollment.value).toEqual(firstEnrollment)
    expect(detail.replace({ ...firstEnrollment, version: 3 })).toBe(true)
    expect(enrollment.value.version).toBe(3)

    detail.dispose()
    expect(detail.drawer.value).toBe(false)
    expect(enrollment.value).toBeNull()
    expect(dependencies.resetGovernance).toHaveBeenCalledOnce()
    expect(detail.open(firstEnrollment)).toBe(false)
  })
})

const itemLocator = 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=106'
const metaItem = { id: 106, engine_id: 11, item_type: 'collection', fingerprint: 'persons-fingerprint' }
const locatedEnrollment = {
  ...firstEnrollment, state: 'active',
  target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: metaItem.fingerprint },
  target_snapshot: { engine_id: 11 }
}
function lookupPage(rows, total = rows.length, page = 1) {
  return { data: rows, total, page, page_size: 100, total_pages: Math.ceil(total / 100) }
}
function createLocatorDetail(overrides = {}) {
  return createDetail({
    getItem: vi.fn().mockResolvedValue(metaItem),
    listEnrollments: vi.fn().mockResolvedValue(lookupPage([locatedEnrollment])),
    getEnrollment: vi.fn().mockResolvedValue(locatedEnrollment),
    onLocatorLoadError: vi.fn(),
    ...overrides
  })
}

describe('resource locator resolves authoritative current protection', () => {
  it('finds a target beyond the first page and refreshes it before opening', async () => {
    const unrelated = Array.from({ length: 100 }, (_, i) => ({ ...locatedEnrollment, id: `other-${i}`, target: { ...locatedEnrollment.target, resource_identity: `other-${i}` } }))
    const { detail, dependencies } = createLocatorDetail({ listEnrollments: vi.fn()
      .mockResolvedValueOnce(lookupPage(unrelated, 101)).mockResolvedValueOnce(lookupPage([locatedEnrollment], 101, 2)) })
    await expect(detail.openLocator(itemLocator)).resolves.toEqual({ status: 'opened', enrollment: locatedEnrollment })
    expect(dependencies.getItem).toHaveBeenCalledWith(106)
    expect(dependencies.listEnrollments).toHaveBeenNthCalledWith(2, { scope: 'current', engine_id: 11, page: 2, page_size: 100 })
    expect(dependencies.getEnrollment).toHaveBeenCalledWith(locatedEnrollment.id)
    expect(dependencies.loadGovernance).toHaveBeenCalledOnce()
  })

  it('reports absence only after a complete current-resource search', async () => {
    const { detail, dependencies } = createLocatorDetail({ listEnrollments: vi.fn().mockResolvedValue(lookupPage([])) })
    await expect(detail.openLocator(itemLocator)).resolves.toEqual({ status: 'not_enrolled' })
    expect(dependencies.getEnrollment).not.toHaveBeenCalled()
    expect(detail.drawer.value).toBe(false)
  })

  it.each([
    ['invalid locator', { locator: 'invalid' }],
    ['Meta identity differs', { getItem: vi.fn().mockResolvedValue({ ...metaItem, engine_id: 2 }) }],
    ['Meta unavailable', { getItem: vi.fn().mockRejectedValue(new Error('403')) }],
    ['list forbidden', { listEnrollments: vi.fn().mockRejectedValue(new Error('403')) }],
    ['missing page data', { listEnrollments: vi.fn().mockResolvedValue(lookupPage([], 1)) }],
    ['wrong page count', { listEnrollments: vi.fn().mockResolvedValue({ ...lookupPage([]), total_pages: 1 }) }],
    ['duplicate targets', { listEnrollments: vi.fn().mockResolvedValue(lookupPage([locatedEnrollment, { ...locatedEnrollment, id: 8 }])) }],
    ['inconsistent target', { listEnrollments: vi.fn().mockResolvedValue(lookupPage([{ ...locatedEnrollment, target_snapshot: { engine_id: 2 } }])) }],
    ['released after lookup', { getEnrollment: vi.fn().mockResolvedValue({ ...locatedEnrollment, state: 'released' }) }],
    ['different refreshed target', { getEnrollment: vi.fn().mockResolvedValue({ ...locatedEnrollment, target: { ...locatedEnrollment.target, resource_identity: 'other' } }) }]
  ])('does not turn %s into absence', async (_, overrides) => {
    const { detail, dependencies } = createLocatorDetail(overrides)
    await expect(detail.openLocator(overrides.locator || itemLocator)).resolves.toMatchObject({ status: 'failed' })
    expect(dependencies.onLocatorLoadError).toHaveBeenCalledOnce()
    expect(detail.drawer.value).toBe(false)
  })

  it('ignores a released record and opens the current lifecycle', async () => {
    const { detail } = createLocatorDetail({ listEnrollments: vi.fn().mockResolvedValue(lookupPage([{ ...locatedEnrollment, id: 8, state: 'released' }, locatedEnrollment])) })
    await expect(detail.openLocator(itemLocator)).resolves.toMatchObject({ status: 'opened' })
    expect(detail.enrollment.value.id).toBe(locatedEnrollment.id)
  })

  it.each(['route', 'dispose', 'newer detail'])('discards a delayed lookup after %s', async kind => {
    let resolveItem
    let current = true
    const { detail, dependencies } = createLocatorDetail({ getItem: vi.fn(() => new Promise(resolve => { resolveItem = resolve })) })
    const opening = detail.openLocator(itemLocator, { isCurrent: () => current })
    if (kind === 'route') current = false
    if (kind === 'dispose') detail.dispose()
    if (kind === 'newer detail') await detail.open(secondEnrollment)
    resolveItem(metaItem)
    await expect(opening).resolves.toEqual({ status: 'stale' })
    expect(dependencies.listEnrollments).not.toHaveBeenCalled()
    expect(dependencies.onLocatorLoadError).not.toHaveBeenCalled()
    if (kind === 'newer detail') expect(detail.enrollment.value.id).toBe(secondEnrollment.id)
  })
})
