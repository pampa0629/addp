import assert from 'node:assert/strict'
import test from 'node:test'

import {
  listSystemCatalogChildren,
  normalizeCatalogPath
} from '../src/api/metaCatalog.js'

test('listSystemCatalogChildren keeps the HTTP client separate from engine id', async () => {
  const requests = []
  const httpClient = {
    async post(url, payload) {
      requests.push({ url, payload })
      return { nodes: [{ name: 'default' }] }
    }
  }

  const nodes = await listSystemCatalogChildren(
    httpClient,
    17,
    { segments: [{ term: 'server', kind: 'server', name: '' }] },
    { limit: 20 }
  )

  assert.deepEqual(nodes, [{ name: 'default' }])
  assert.deepEqual(requests, [{
    url: '/system/engines/17/catalog/children',
    payload: {
      path: { segments: [{ term: 'server', kind: 'server', name: '' }] },
      options: { limit: 20 }
    }
  }])
})

test('normalizeCatalogPath always returns an independent segments array', () => {
  const segments = [{ term: 'database', kind: 'namespace', name: 'default' }]
  const normalized = normalizeCatalogPath({ engine_id: 17, segments })

  assert.deepEqual(normalized, { engine_id: 17, segments })
  assert.notEqual(normalized.segments, segments)
})
