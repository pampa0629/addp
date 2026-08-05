import client from './client'

export function getEmbeddingConfiguration() {
  return client.get('/manager/settings/embedding')
}

export function updateEmbeddingConfiguration(payload) {
  return client.put('/manager/settings/embedding', payload)
}
