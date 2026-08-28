import { beforeEach, describe, expect, it, vi } from 'vitest'

const { clientGet } = vi.hoisted(() => ({ clientGet: vi.fn() }))

vi.mock('@/api/client', () => ({ default: { get: clientGet } }))

import { getCatalogEntryBySourceIdentity } from '@/api/catalog'

describe('Catalog API', () => {
  beforeEach(() => {
    clientGet.mockReset()
  })

  it('queries the single catalog entry bound to a Meta source identity', () => {
    getCatalogEntryBySourceIdentity('item-fingerprint')

    expect(clientGet).toHaveBeenCalledWith('/catalog/entries', {
      params: {
        source_identity: 'item-fingerprint',
        page: 1,
        page_size: 1
      }
    })
  })

  it('requests the inventory view only when the caller has inventory permission', () => {
    getCatalogEntryBySourceIdentity('item-fingerprint', { includeInventory: true })

    expect(clientGet).toHaveBeenCalledWith('/catalog/entries', {
      params: {
        view: 'inventory',
        source_identity: 'item-fingerprint',
        page: 1,
        page_size: 1
      }
    })
  })
})
