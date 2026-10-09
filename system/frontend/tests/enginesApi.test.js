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
    get: vi.fn(), post: vi.fn()
  }
}))

describe('engines API', () => {
  beforeEach(() => {
    client.get.mockReset()
    client.post.mockReset()
  })

  it('uses explicit approval configuration and live discovery APIs without Meta or grant writes', async () => {
    const body = '{"catalog_path":{"engine_id":9007199254740993,"version":"v1","segments":[]},"mode":"independent","reason":"Configure"}'
    await enginesAPI.listApprovalRequirements('9007199254740993', { page: 2, page_size: 10 })
    await enginesAPI.initializeApprovalRequirement('9007199254740993', body)
    await enginesAPI.listCatalogChildren('2', { version: 'v1', engine_id: 2, segments: [] })
    expect(client.get).toHaveBeenCalledWith('/system/engines/9007199254740993/access_approval_requirements', { params: { page: 2, page_size: 10 } })
    expect(client.post.mock.calls).toEqual([
      ['/system/engines/9007199254740993/access_approval_requirements', body, { headers: { 'Content-Type': 'application/json' } }],
      ['/system/engines/2/catalog/children', { path: { version: 'v1', engine_id: 2, segments: [] } }]
    ])
  })

  it('uses the canonical delegation endpoints and preserves explicit write parameters', async () => {
    const create = { tenant_membership_id: '9007199254740993', expires_at: '2099-01-01T00:00:00Z', reason: 'Delegate' }
    const revoke = { version: 7, reason: 'Revoke' }
    await enginesAPI.listAccessDelegations('2', { page: 2, page_size: 10 })
    await enginesAPI.createAccessDelegation('2', create)
    await enginesAPI.revokeAccessDelegation('2', '3', revoke)
    expect(client.get).toHaveBeenCalledWith('/system/engines/2/access_delegations', { params: { page: 2, page_size: 10 } })
    expect(client.post.mock.calls).toEqual([['/system/engines/2/access_delegations', create], ['/system/engines/2/access_delegations/3/revoke', revoke]])
  })

  it('serializes inspection catalog IDs as lossless JSON numbers while keeping account IDs as strings', async () => {
    const target = { engine_id: '9007199254740993', version: 'catalog.path/v1', segments: [{ term: 'server', kind: 'server', name: '' }, { term: 'table', kind: 'table', name: 'orders' }] }
    await enginesAPI.inspectSourceGrants('9007199254740993', { account_id: '9007199254740995', catalog_path: target })
    expect(client.post).toHaveBeenCalledWith('/system/engines/9007199254740993/access_grants/inspection',
      '{"account_id":"9007199254740995","catalog_path":{"engine_id":9007199254740993,"version":"catalog.path/v1","segments":[{"term":"server","kind":"server","name":""},{"term":"table","kind":"table","name":"orders"}]}}',
      { headers: { 'Content-Type': 'application/json' } })
  })

  it('separates current relations from immutable history and withdraws by an anchor', async () => {
    const params = { page: 2, page_size: 20 }
    await enginesAPI.listSourceGrants('2', params)
    await enginesAPI.listSourceGrantHistory('2', params)
    await enginesAPI.revokeSourceGrant('2', 'anchor-id', 'Withdraw the relation')
    expect(client.get.mock.calls).toEqual([
      ['/system/engines/2/access_grants', { params }],
      ['/system/engines/2/access_grants/history', { params }]
    ])
    expect(client.post.mock.calls).toEqual([
      ['/system/engines/2/access_grants/anchor-id/revoke', { reason: 'Withdraw the relation' }]
    ])
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

  it('loads registerable engine types from the plugin descriptor endpoint', async () => {
    client.get.mockResolvedValue([])

    await enginesAPI.listTypes()

    expect(client.get).toHaveBeenCalledWith('/system/engine-types')
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
