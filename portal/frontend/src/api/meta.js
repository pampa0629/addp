import client from './client'

const unwrap = (promise, defaultValue) =>
  promise.then(res => res.data?.data ?? defaultValue)

export const getResources = () => unwrap(client.get('/meta/resources'), [])

export const getSchemas = resourceId =>
  unwrap(client.get(`/meta/schemas/${resourceId}`), [])

export const listAvailableSchemas = resourceId =>
  unwrap(client.get(`/meta/schemas/${resourceId}/available`), [])

export const autoScan = () => client.post('/meta/scan/auto').then(res => res.data)

export const scanResource = (resourceId, schemaNames) =>
  client
    .post('/meta/scan/resource', {
      resource_id: resourceId,
      schema_names: schemaNames
    })
    .then(res => res.data)

export default {
  getResources,
  getSchemas,
  listAvailableSchemas,
  autoScan,
  scanResource
}
