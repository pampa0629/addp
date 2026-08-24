import { beforeEach, describe, expect, it, vi } from 'vitest'

import client from '../src/api/client'
import { enginesAPI } from '../src/api/engines'
import {
  ENGINE_DELETION_REFRESH_INTERVAL_MS,
  ENGINE_STATUS_REFRESH_INTERVAL_MS,
  getEngineRefreshInterval,
  paginateEngines
} from '../src/utils/engineList'

vi.mock('../src/api/client', () => ({
  default: {
    get: vi.fn()
  }
}))

describe('engines API', () => {
  beforeEach(() => {
    client.get.mockReset()
  })

  it('requests the complete filtered engine array without pagination parameters', async () => {
    client.get.mockResolvedValue([])

    await enginesAPI.list({
      capabilityGroups: ['storage', 'compute'],
      engineOrigins: ['general'],
      lifecycleStates: ['active', 'disabled'],
      includeBuiltin: false
    })

    expect(client.get).toHaveBeenCalledWith('/system/engines', {
      params: {
        capability_groups: 'storage,compute',
        engine_origins: 'general',
        lifecycle_states: 'active,disabled',
        include_builtin: false
      }
    })
  })
})

describe('engine management pagination', () => {
  const engines = Array.from({ length: 12 }, (_, index) => ({ id: index + 1 }))

  it('returns the selected local page', () => {
    expect(paginateEngines(engines, 2, 5).map(engine => engine.id)).toEqual([6, 7, 8, 9, 10])
  })

  it('returns the remaining engines on the last page', () => {
    expect(paginateEngines(engines, 3, 5).map(engine => engine.id)).toEqual([11, 12])
  })
})

describe('engine management live refresh', () => {
  it('refreshes connection observations periodically', () => {
    expect(getEngineRefreshInterval([{ lifecycle_state: 'active' }]))
      .toBe(ENGINE_STATUS_REFRESH_INTERVAL_MS)
  })

  it('uses a shorter interval while deletion cleanup is running', () => {
    expect(getEngineRefreshInterval([
      { lifecycle_state: 'active' },
      { lifecycle_state: 'deleting' }
    ])).toBe(ENGINE_DELETION_REFRESH_INTERVAL_MS)
  })
})
