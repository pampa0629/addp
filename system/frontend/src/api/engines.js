import client from './client'
import { serializeEngineCatalogTarget } from '@common-ui'

export const enginesAPI = {
  createSourceGrant: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_grants`, payload, { headers: { 'Content-Type': 'application/json' } }),
  listSourceGrants: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_grants`, { params }),
  listSourceGrantHistory: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_grants/history`, { params }),
  inspectSourceGrants: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_grants/inspection`,
    `{"account_id":${JSON.stringify(payload.account_id)},"catalog_path":${serializeEngineCatalogTarget(payload.catalog_path)}}`,
    { headers: { 'Content-Type': 'application/json' } }),
  revokeSourceGrant: (id, requestID, reason) => client.post(`/system/engines/${encodeURIComponent(id)}/access_grants/${encodeURIComponent(requestID)}/revoke`, { reason }),
  listApprovalRequirements: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_approval_requirements`, { params }),
  getApprovalRequirement: (id, requirementID) => client.get(`/system/engines/${encodeURIComponent(id)}/access_approval_requirements/${encodeURIComponent(requirementID)}`),
  updateApprovalRequirement: (id, requirementID, payload) => client.put(`/system/engines/${encodeURIComponent(id)}/access_approval_requirements/${encodeURIComponent(requirementID)}`, payload),
  initializeApprovalRequirement: (id, serializedPayload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_approval_requirements`, serializedPayload, { headers: { 'Content-Type': 'application/json' } }),
  listCatalogChildren: (id, path = { segments: [] }) => client.post(`/system/engines/${encodeURIComponent(id)}/catalog/children`, { path }),
  listAccessDelegations: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_delegations`, { params }),
  createAccessDelegation: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_delegations`, payload),
  revokeAccessDelegation: (id, delegationID, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_delegations/${encodeURIComponent(delegationID)}/revoke`, payload),
  listTypes: () => client.get('/system/engine-types'),

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

  restore: (id, data) => {
    return client.post(`/system/engines/${id}/restore`, data)
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
