import { describe, expect, it, vi } from 'vitest'

import { fetchPortalStatus } from '../src/utils/portalStatus'

describe('fetchPortalStatus', () => {
  it('requests only status APIs allowed by the current AuthContext permissions', async () => {
    const client = {
      get: vi.fn(async url => url === '/system/engines'
        ? [{ id: 1 }, { id: 2 }]
        : { data: { total: url.length } })
    }

    const status = await fetchPortalStatus(client, [
      'system.engine.read',
      'monitor.execution.read'
    ])

    expect(client.get).toHaveBeenCalledTimes(2)
    expect(client.get).toHaveBeenNthCalledWith(1, '/system/engines')
    expect(client.get).toHaveBeenNthCalledWith(2, '/monitor/executions?status=running&page_size=1')
    expect(status).toEqual({
      engines: 2,
      datasets: null,
      services: null,
      tasks: '/monitor/executions?status=running&page_size=1'.length
    })
  })

  it('returns null for a permitted status request that fails', async () => {
    const client = { get: vi.fn(async () => { throw new Error('unavailable') }) }

    await expect(fetchPortalStatus(client, ['meta.catalog.read'])).resolves.toEqual({
      engines: null,
      datasets: null,
      services: null,
      tasks: null
    })
  })
})
