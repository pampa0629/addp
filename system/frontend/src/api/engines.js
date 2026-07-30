import client from './client'

export const enginesAPI = {
  create: (data) => {
    return client.post('/system/engines', data)
  },

  list: (page = 1, pageSize = 10, filters = {}) => {
    const params = { page, page_size: pageSize }
    if (filters.engineType) params.engine_type = filters.engineType
    if (filters.capabilityGroups?.length) params.capability_groups = filters.capabilityGroups.join(',')
    if (filters.engineOrigins?.length) params.engine_origins = filters.engineOrigins.join(',')
    if (filters.lifecycleStates?.length) params.lifecycle_states = filters.lifecycleStates.join(',')
    if (filters.includeBuiltin === false) params.include_builtin = false
    return client.get('/system/engines', { params })
  },

  getById: (id) => {
    return client.get(`/system/engines/${id}`)
  },

  update: (id, data) => {
    return client.put(`/system/engines/${id}`, data)
  },

  delete: (id, externalArtifactPolicy = 'delete') => {
    return client.delete(`/system/engines/${id}`, {
      data: { external_artifact_policy: externalArtifactPolicy }
    })
  },

  testConnection: (data) => {
    return client.post('/system/engines/test-connection', data)
  },

  testConnectionBeforeCreate: (data) => {
    return client.post('/system/engines/test-connection', data)
  },

  testExistingConnection: (id, data = null) => {
    if (data) {
      return client.post(`/system/engines/${id}/test`, data)
    }
    return client.post(`/system/engines/${id}/test`)
  },

  enableSpatialWorkspace: (id, ecosystem, kind) => {
    return client.post(`/system/engines/${id}/spatial-workspaces/${encodeURIComponent(ecosystem)}/${encodeURIComponent(kind)}/enable`)
  }
}
