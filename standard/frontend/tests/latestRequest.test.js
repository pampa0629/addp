import { describe, expect, it } from 'vitest'
import { createLatestRequestCoordinator } from '@common-ui'

describe('latest request coordination', () => {
  it('只接受最新查询快照的响应', () => {
    const coordinator = createLatestRequestCoordinator()
    const first = coordinator.begin('{"keyword":"old"}')
    const second = coordinator.begin('{"keyword":"new"}')

    expect(coordinator.isCurrent(first, '{"keyword":"old"}')).toBe(false)
    expect(coordinator.isCurrent(second, '{"keyword":"new"}')).toBe(true)
  })

  it('失效后不再接受之前已经发出的请求', () => {
    const coordinator = createLatestRequestCoordinator()
    const request = coordinator.begin('initial')

    coordinator.invalidate()

    expect(coordinator.isCurrent(request, 'initial')).toBe(false)
  })
})
