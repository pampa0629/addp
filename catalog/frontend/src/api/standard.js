import client from './client'

// Professional definitions remain owned and authorized by Standard. This client
// uses the current user's Catalog session, never the Catalog service identity.
export function listDomainGlossaries(params) {
  return client.get('/standard/glossaries', { params })
}

export function listDomainElements(params) {
  return client.get('/standard/elements', { params })
}

export function listDomainMetrics(params) {
  return client.get('/standard/metrics', { params })
}
