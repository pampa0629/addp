import client from './client'

export function getCatalogEntryBySourceIdentity(sourceIdentity, { includeInventory = false } = {}) {
  return client.get('/catalog/entries', {
    params: {
      ...(includeInventory ? { view: 'inventory' } : {}),
      source_identity: sourceIdentity,
      page: 1,
      page_size: 1
    }
  })
}
