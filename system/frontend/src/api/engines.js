import client from './client'

export const enginesAPI = {
  create: (data) => {
    return client.post('/system/engines', data)
  },

  list: (filters = {}) => {
    const params = {}
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

  createDeletionAssessment: (id, data) => {
    return client.post(`/system/engines/${id}/deletion-assessments`, data)
  },

  getDeletionAssessment: (id, assessmentId) => {
    return client.get(`/system/engines/${id}/deletion-assessments/${encodeURIComponent(assessmentId)}`)
  },

  delete: (id, data) => {
    return client.delete(`/system/engines/${id}`, {
      data
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
