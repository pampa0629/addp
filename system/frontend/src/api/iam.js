import client from './client'

function list(path, params) {
  return client.get(path, { params })
}

function exportAudit(path, params) {
  return client.get(path, { params, responseType: 'blob' })
}

export const iamAPI = {
  platformTenants: {
    list: (params) => list('/system/platform/tenants', params),
    create: (data) => client.post('/system/platform/tenants', data),
    update: (id, data) => client.put(`/system/platform/tenants/${id}`, data),
    suspend: (id, reason) => client.post(`/system/platform/tenants/${id}/suspend`, { reason }),
    restore: (id, reason) => client.post(`/system/platform/tenants/${id}/restore`, { reason }),
    close: (id, reason) => client.post(`/system/platform/tenants/${id}/close`, { reason })
  },
  platformUsers: {
    list: (params) => list('/system/platform/users', params),
    create: (data) => client.post('/system/platform/users', data),
    update: (id, data) => client.put(`/system/platform/users/${id}`, data),
    suspend: (id, data) => client.post(`/system/platform/users/${id}/suspend`, data),
    reactivate: (id, data) => client.post(`/system/platform/users/${id}/reactivate`, data)
  },
  identityChanges: {
    list: (params) => list('/system/platform/identity_changes', params),
    create: (data) => client.post('/system/platform/identity_changes', data),
    approve: (id, reason) => client.post(`/system/platform/identity_changes/${id}/approve`, { reason }),
    reject: (id, reason) => client.post(`/system/platform/identity_changes/${id}/reject`, { reason })
  },
  memberships: {
    list: (params) => list('/system/tenant/memberships', params),
    update: (id, expiresAt) => client.put(`/system/tenant/memberships/${id}`, { expires_at: expiresAt }),
    suspend: (id, reason) => client.post(`/system/tenant/memberships/${id}/suspend`, { reason }),
    restore: (id, reason) => client.post(`/system/tenant/memberships/${id}/restore`, { reason }),
    close: (id, reason) => client.post(`/system/tenant/memberships/${id}/close`, { reason })
  },
  invitations: {
    list: (params) => list('/system/tenant/invitations', params),
    create: (email) => client.post('/system/tenant/invitations', { email }),
    revoke: (id) => client.post(`/system/tenant/invitations/${id}/revoke`)
  },
  audit: {
    list: (scope, params) => list(`/system/${scope}/audit/events`, params),
    summary: (scope, params) => client.get(`/system/${scope}/audit/events/summary`, { params }),
    trends: (scope, params) => client.get(`/system/${scope}/audit/events/trends`, { params }),
    export: (scope, params) => exportAudit(`/system/${scope}/audit/events/export`, params)
  }
}
