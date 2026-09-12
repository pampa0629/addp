import { describe, expect, it, vi } from 'vitest'
import { useProtectionReferenceCatalog } from '../src/composables/useProtectionReferenceCatalog.mjs'

function deferred() {
  let resolve
  let reject
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function createCatalog(overrides = {}) {
  const dependencies = {
    listSensitiveTypes: vi.fn().mockResolvedValue([{ id: 1, name: 'Phone' }]),
    listClassifications: vi.fn().mockResolvedValue([{ id: 2, name: 'Personal' }]),
    listGrades: vi.fn().mockResolvedValue([{ id: 3, name: 'High' }]),
    listBaselines: vi.fn().mockResolvedValue([{ id: 4, effect: 'mask' }]),
    listDetectorCapabilities: vi.fn().mockResolvedValue([{ key: 'phone_metadata' }]),
    listEngines: vi.fn().mockResolvedValue([{ id: 5, name: 'Business MySQL' }]),
    ...overrides
  }
  return {
    dependencies,
    catalog: useProtectionReferenceCatalog(dependencies)
  }
}

describe('protected-resource reference catalog', () => {
  it('deduplicates concurrent definition loads and caches valid empty results', async () => {
    const types = deferred()
    const { catalog, dependencies } = createCatalog({
      listSensitiveTypes: vi.fn(() => types.promise),
      listClassifications: vi.fn().mockResolvedValue([]),
      listGrades: vi.fn().mockResolvedValue([]),
      listBaselines: vi.fn().mockResolvedValue([])
    })

    const first = catalog.loadDefinitions()
    const second = catalog.loadDefinitions()
    expect(second).toBe(first)
    expect(dependencies.listSensitiveTypes).toHaveBeenCalledOnce()
    expect(dependencies.listClassifications).toHaveBeenCalledOnce()
    types.resolve([])

    await expect(first).resolves.toBe(true)
    await expect(catalog.loadDefinitions()).resolves.toBe(true)
    expect(dependencies.listSensitiveTypes).toHaveBeenCalledOnce()
    expect(catalog.sensitiveTypes.value).toEqual([])
    expect(catalog.securityClassifications.value).toEqual([])
  })

  it('clears a failed definition request so the catalog can retry', async () => {
    const error = new Error('definitions unavailable')
    const { catalog, dependencies } = createCatalog({
      listSensitiveTypes: vi.fn()
        .mockRejectedValueOnce(error)
        .mockResolvedValueOnce([{ id: 7, name: 'Email' }])
    })

    await expect(catalog.loadDefinitions()).rejects.toBe(error)
    await expect(catalog.loadDefinitions()).resolves.toBe(true)

    expect(dependencies.listSensitiveTypes).toHaveBeenCalledTimes(2)
    expect(catalog.sensitiveTypes.value).toEqual([{ id: 7, name: 'Email' }])
  })

  it('keeps a newer policy baseline when an earlier definition request completes', async () => {
    const baselines = deferred()
    const { catalog } = createCatalog({ listBaselines: vi.fn(() => baselines.promise) })

    const loading = catalog.loadDefinitions()
    const latest = [{ id: 9, effect: 'deny' }]
    expect(catalog.setProtectionBaselines(latest)).toBe(true)
    baselines.resolve([{ id: 4, effect: 'mask' }])

    await expect(loading).resolves.toBe(true)
    expect(catalog.protectionBaselines.value).toEqual(latest)
  })

  it('deduplicates detector capability loads and caches an empty catalog', async () => {
    const capabilities = deferred()
    const { catalog, dependencies } = createCatalog({
      listDetectorCapabilities: vi.fn(() => capabilities.promise)
    })

    const first = catalog.loadDetectorCapabilities()
    const second = catalog.loadDetectorCapabilities()
    expect(second).toBe(first)
    capabilities.resolve([])

    await expect(first).resolves.toBe(true)
    await expect(catalog.loadDetectorCapabilities()).resolves.toBe(true)
    expect(dependencies.listDetectorCapabilities).toHaveBeenCalledOnce()
    expect(catalog.detectorCapabilities.value).toEqual([])
  })

  it('maps engine names, swallows an initial failure, and allows retry', async () => {
    const { catalog, dependencies } = createCatalog({
      listEngines: vi.fn()
        .mockRejectedValueOnce(new Error('engines unavailable'))
        .mockResolvedValueOnce([{ id: '12', name: 'Meta PostgreSQL' }])
    })

    await expect(catalog.loadEngines()).resolves.toBe(false)
    expect(catalog.engineNames.value).toEqual(new Map())
    await expect(catalog.loadEngines()).resolves.toBe(true)

    expect(dependencies.listEngines).toHaveBeenCalledTimes(2)
    expect(catalog.engineNames.value.get(12)).toBe('Meta PostgreSQL')
  })

  it('rejects late writes after disposal', async () => {
    const types = deferred()
    const capabilities = deferred()
    const engines = deferred()
    const { catalog } = createCatalog({
      listSensitiveTypes: vi.fn(() => types.promise),
      listDetectorCapabilities: vi.fn(() => capabilities.promise),
      listEngines: vi.fn(() => engines.promise)
    })

    const definitionsLoad = catalog.loadDefinitions()
    const capabilitiesLoad = catalog.loadDetectorCapabilities()
    const enginesLoad = catalog.loadEngines()
    catalog.dispose()
    types.resolve([{ id: 11 }])
    capabilities.resolve([{ key: 'late' }])
    engines.resolve([{ id: 13, name: 'Late engine' }])

    await expect(definitionsLoad).resolves.toBe(false)
    await expect(capabilitiesLoad).resolves.toBe(false)
    await expect(enginesLoad).resolves.toBe(false)
    expect(catalog.sensitiveTypes.value).toEqual([])
    expect(catalog.detectorCapabilities.value).toEqual([])
    expect(catalog.engineNames.value).toEqual(new Map())
    expect(catalog.setProtectionBaselines([{ id: 14 }])).toBe(false)
  })
})
