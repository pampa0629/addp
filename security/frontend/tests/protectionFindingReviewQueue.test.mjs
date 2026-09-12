import { describe, expect, it, vi } from 'vitest'
import {
  resolveFindingReviewQueueRouteState,
  useProtectionFindingReviewQueue
} from '../src/composables/useProtectionFindingReviewQueue.mjs'

function createQueue(overrides = {}) {
  const dependencies = {
    canRead: true,
    initialRouteState: resolveFindingReviewQueueRouteState(),
    listFindings: vi.fn().mockResolvedValue({ data: [{ id: 'finding-1' }], total: 1, total_pages: 1 }),
    onRefreshed: vi.fn(),
    onLoadError: vi.fn(),
    ...overrides
  }
  return { dependencies, queue: useProtectionFindingReviewQueue(dependencies) }
}

describe('protection finding review queue', () => {
  it('owns the canonical recoverable route state', () => {
    const routeState = resolveFindingReviewQueueRouteState({
      sensitive_data_type_id: '9',
      detector_version: ' addp.detector.phone_metadata/v2 ',
      page: '3',
      page_size: '50'
    })
    const { queue } = createQueue({ initialRouteState: routeState })

    expect(queue.routeQuery()).toEqual({
      tab: 'review-queue',
      sensitive_data_type_id: '9',
      detector_version: 'addp.detector.phone_metadata/v2',
      page: '3',
      page_size: '50'
    })

    queue.applyRouteState(resolveFindingReviewQueueRouteState({ detector_version: 'metadata/v3' }))
    expect(queue.sensitiveDataTypeID.value).toBe('')
    expect(queue.detectorVersion.value).toBe('metadata/v3')
    expect(queue.page.value).toBe(1)
    expect(queue.pageSize.value).toBe(20)
  })

  it('loads only current pending findings and corrects an out-of-range page', async () => {
    const listFindings = vi.fn()
      .mockResolvedValueOnce({ data: [], total: 21, total_pages: 3 })
      .mockResolvedValueOnce({ data: [{ id: 'finding-21' }], total: 21, total_pages: 3 })
    const onRefreshed = vi.fn()
    const { queue } = createQueue({
      initialRouteState: resolveFindingReviewQueueRouteState({
        sensitive_data_type_id: '9',
        detector_version: 'metadata/v2',
        page_size: '50'
      }),
      listFindings,
      onRefreshed
    })

    await expect(queue.loadQueue(5)).resolves.toBe(true)

    expect(listFindings).toHaveBeenNthCalledWith(1, {
      snapshot_scope: 'current',
      review_state: 'pending',
      sensitive_data_type_id: '9',
      detector_version: 'metadata/v2',
      page: 5,
      page_size: 50
    })
    expect(listFindings).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 3 }))
    expect(queue.page.value).toBe(3)
    expect(queue.rows.value).toEqual([{ id: 'finding-21' }])
    expect(queue.total.value).toBe(21)
    expect(onRefreshed).toHaveBeenCalledOnce()
  })

  it('resets filter pagination and clears filters without fetching on its own', () => {
    const { dependencies, queue } = createQueue({
      initialRouteState: resolveFindingReviewQueueRouteState({
        sensitive_data_type_id: '9',
        detector_version: 'metadata/v2',
        page: '3'
      })
    })

    queue.prepareFilterChange()
    expect(queue.page.value).toBe(1)

    queue.page.value = 4
    queue.resetFilters()
    expect(queue.sensitiveDataTypeID.value).toBe('')
    expect(queue.detectorVersion.value).toBe('')
    expect(queue.page.value).toBe(1)
    expect(dependencies.listFindings).not.toHaveBeenCalled()
  })

  it('continues at the row filled by the next pending candidate', async () => {
    const { queue } = createQueue({
      listFindings: vi.fn().mockResolvedValue({
        data: [{ id: 'finding-b' }, { id: 'finding-c' }],
        total: 2,
        total_pages: 1
      })
    })
    queue.rows.value = [{ id: 'finding-a' }, { id: 'finding-b' }, { id: 'finding-c' }]

    await expect(queue.nextFindingAfterReview({ id: 'finding-a' })).resolves.toEqual({
      finding: { id: 'finding-b' },
      pageChanged: false
    })
  })

  it('moves to the previous page when reviewing empties the old last page', async () => {
    const listFindings = vi.fn()
      .mockResolvedValueOnce({ data: [], total: 40 })
      .mockResolvedValueOnce({ data: [{ id: 'finding-40' }], total: 40 })
    const { queue } = createQueue({
      initialRouteState: resolveFindingReviewQueueRouteState({ page: '3' }),
      listFindings
    })
    queue.rows.value = [{ id: 'finding-41' }]

    await expect(queue.nextFindingAfterReview({ id: 'finding-41' })).resolves.toEqual({
      finding: { id: 'finding-40' },
      pageChanged: true
    })
    expect(listFindings).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 2 }))
    expect(queue.page.value).toBe(2)
  })

  it('ignores stale responses and invalidates in-flight work on disposal', async () => {
    const pending = []
    const listFindings = vi.fn(() => new Promise(resolve => pending.push(resolve)))
    const { queue } = createQueue({ listFindings })

    const firstLoad = queue.loadQueue(1)
    const secondLoad = queue.loadQueue(2)
    pending[1]({ data: [{ id: 'finding-2' }], total: 2, total_pages: 2 })
    await expect(secondLoad).resolves.toBe(true)
    pending[0]({ data: [{ id: 'finding-1' }], total: 2, total_pages: 2 })
    await expect(firstLoad).resolves.toBe(false)
    expect(queue.rows.value).toEqual([{ id: 'finding-2' }])

    const disposedLoad = queue.loadQueue(2)
    queue.dispose()
    pending[2]({ data: [{ id: 'finding-stale' }], total: 2, total_pages: 2 })
    await expect(disposedLoad).resolves.toBe(false)
    expect(queue.rows.value).toEqual([{ id: 'finding-2' }])
    expect(queue.loading.value).toBe(false)
  })
})
