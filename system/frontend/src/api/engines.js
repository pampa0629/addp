import client from './client'
import { ENGINE_LIFECYCLE_STATES } from '../utils/engineList'

export const enginesAPI = {
  getMetaCollectionTaskFacts: (id, taskID) => client.get(`/system/engines/${encodeURIComponent(id)}/meta_collection_task_facts/${encodeURIComponent(taskID)}`),
  listMetaCollectionQualifications: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/meta_collection_qualifications`, { params }),
  approveMetaCollection: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/meta_collection_qualifications`, payload),
  revokeMetaCollection: (id, qualificationID, payload) => client.delete(`/system/engines/${encodeURIComponent(id)}/meta_collection_qualifications/${encodeURIComponent(qualificationID)}`, { data: payload }),
	getAuthorizationFacts: (id, fingerprint) => client.get(`/system/engines/${encodeURIComponent(id)}/data-items/${encodeURIComponent(fingerprint)}/authorization-facts`),
  createSourceGrant: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_grants`, payload, { headers: { 'Content-Type': 'application/json' } }),
  listSourceGrants: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_grants`, { params }),
  listSourceGrantHistory: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_grants/history`, { params }),
  revokeSourceGrant: (id, requestID, reason) => client.post(`/system/engines/${encodeURIComponent(id)}/access_grants/${encodeURIComponent(requestID)}/revoke`, { reason }),
  createSourceDeny: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_denies`, payload, { headers: { 'Content-Type': 'application/json' } }),
  releaseSourceDeny: (id, denyID, reason) => client.post(`/system/engines/${encodeURIComponent(id)}/access_denies/${encodeURIComponent(denyID)}/release`, { reason }),
  listDirectPermissions: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/direct-permissions`, { params }),
  getDirectPermission: (id, fingerprint) => client.get(`/system/engines/${encodeURIComponent(id)}/data-items/${encodeURIComponent(fingerprint)}/direct-permission`),
  setDirectPermission: (id, fingerprint, payload) => client.put(`/system/engines/${encodeURIComponent(id)}/data-items/${encodeURIComponent(fingerprint)}/direct-permission`, payload, { headers: { 'Content-Type': 'application/json' } }),
  listCatalogChildren: (id, path = { segments: [] }) => client.post(`/system/engines/${encodeURIComponent(id)}/catalog/children`, { path }),
  listAccessDelegations: (id, params) => client.get(`/system/engines/${encodeURIComponent(id)}/access_delegations`, { params }),
  createAccessDelegation: (id, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_delegations`, payload),
  revokeAccessDelegation: (id, delegationID, payload) => client.post(`/system/engines/${encodeURIComponent(id)}/access_delegations/${encodeURIComponent(delegationID)}/revoke`, payload),
  listTypes: () => client.get('/system/engine-types'),

  create: (data) => {
    return client.post('/system/engines', data)
  },

  list: () => client.get('/system/engines', {
    params: { lifecycle_states: ENGINE_LIFECYCLE_STATES.join(',') }
  }),

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
