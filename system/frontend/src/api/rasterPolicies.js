import client from './client'

function endpoint(scope) {
  if (!['platform', 'tenant'].includes(scope)) throw new Error('Invalid configuration context')
  return `/system/${scope}/engine-raster-policies`
}
export const rasterPoliciesAPI = {
  engines: scope => client.get(`${endpoint(scope)}/engines`),
  get: (scope, id) => client.get(`${endpoint(scope)}/${id}`),
  save: (scope, id, body) => client.put(`${endpoint(scope)}/${id}`, body)
}
