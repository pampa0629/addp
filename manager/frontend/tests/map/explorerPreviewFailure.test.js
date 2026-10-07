import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useExplorerStore } from '../../src/stores/explorer'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ default: { get: mocks.get } }))

const locator = 'addp://engine/2/path/example/first?type=table&item_id=8'
const otherLocator = 'addp://engine/2/path/example/second?type=table&item_id=10'
const preview = { mode: 'table', columns: ['id'], rows: [{ id: 1 }], total: 1 }
const failure = status => ({ response: { status, data: { error: 'request refused' } } })

describe('explorer preview failure state', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it.each([403, 500, 503])('keeps HTTP %s distinct from an empty preview and clears old data', async status => {
    const store = useExplorerStore()
    store.previewData = preview
    mocks.get.mockRejectedValueOnce(failure(status))
    await expect(store.loadPreview(locator)).rejects.toMatchObject({ response: { status } })
    expect(store.previewError).toEqual({ status })
    expect(store.previewData).toBeNull()
    expect(store.previewLoading).toBe(false)
  })

  it('clears the failure when a new request starts, including successful empty results', async () => {
    const store = useExplorerStore()
    mocks.get.mockRejectedValueOnce(failure(403))
    await expect(store.loadPreview(locator)).rejects.toBeDefined()
    let complete
    mocks.get.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const request = store.loadPreview(locator)
    expect(store.previewError).toBeNull()
    complete({ mode: 'table', columns: ['id'], rows: [], total: 0 })
    await request
    expect(store.previewError).toBeNull()
    expect(store.previewData.rows).toEqual([])
  })

  it('does not let a stale rejection overwrite the current successful preview', async () => {
    const store = useExplorerStore()
    let rejectOld
    mocks.get.mockImplementationOnce(() => new Promise((resolve, reject) => { rejectOld = reject }))
    const oldRequest = store.loadPreview(locator)
    const oldResult = expect(oldRequest).resolves.toBeNull()
    mocks.get.mockResolvedValueOnce(preview)
    await store.loadPreview(otherLocator)
    rejectOld(failure(403))
    await oldResult
    expect(store.previewError).toBeNull()
    expect(store.previewData).toEqual(preview)
  })

  it('tracks child failure without displaying the previous child content', async () => {
    const store = useExplorerStore()
    store.previewData = { object: { content: { kind: 'container', json: { children: [] } } } }
    store.activeChildPreviewData = preview
    mocks.get.mockRejectedValueOnce(failure(403))
    await expect(store.loadPreview(locator, 1, 'child')).rejects.toBeDefined()
    expect(store.previewError).toEqual({ status: 403 })
    expect(store.activeChildPreviewData).toBeNull()
    expect(store.childPreviewLoading).toBe(false)
    store.clearPreview()
    expect(store.previewError).toBeNull()
  })
})
