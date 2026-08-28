import client from './client'

function list(path, params) {
  return client.get(path, { params })
}

function exportAudit(path, params) {
  return client.get(path, { params, responseType: 'blob' })
}

export const iamAPI = {
  mfa: {
    status: () => client.get('/system/auth/mfa'),
    beginEnrollment: (currentPassword) => client.post('/system/auth/mfa/totp-enrollments', {
      current_password: currentPassword
    }, { withCredentials: true }),
    completeEnrollment: (enrollmentToken, code) => client.post('/system/auth/mfa/totp-enrollment-verifications', {
      enrollment_token: enrollmentToken,
      code
    }, { withCredentials: true }),
    beginStepUp: () => client.post('/system/auth/mfa/step-up-challenges', null, { withCredentials: true }),
    completeStepUp: (challengeToken, code) => client.post('/system/auth/mfa/step-up-verifications', {
      challenge_token: challengeToken,
      code
    }, { withCredentials: true })
  },
  platformTenants: {
    list: (params) => list('/system/platform/tenants', params),
    listAdministratorCandidates: (params) => list('/system/platform/tenant_administrator_candidates', params),
    create: (data) => client.post('/system/platform/tenants', data),
    initialize: (id, initialAdministratorPrincipalId) => client.post(`/system/platform/tenants/${id}/initialization`, {
      initial_administrator_principal_id: initialAdministratorPrincipalId
    }),
    update: (id, data) => client.put(`/system/platform/tenants/${id}`, data),
    suspend: (id, reason) => client.post(`/system/platform/tenants/${id}/suspend`, { reason }),
    restore: (id, reason) => client.post(`/system/platform/tenants/${id}/restore`, { reason }),
    close: (id, reason) => client.post(`/system/platform/tenants/${id}/close`, { reason })
  },
  platformUsers: {
    list: (params) => list('/system/platform/users', params),
    create: (data) => client.post('/system/platform/users', data),
    update: (id, data) => client.put(`/system/platform/users/${id}`, data),
    resetPassword: (id, data) => client.post(`/system/platform/users/${id}/reset-password`, data),
    resetMFA: (id, data) => client.post(`/system/platform/users/${id}/reset-mfa`, data),
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
  departments: {
    list: (params) => list('/system/tenant/departments', params),
    get: (id) => client.get(`/system/tenant/departments/${id}`),
    create: (data) => client.post('/system/tenant/departments', data),
    update: (id, data) => client.put(`/system/tenant/departments/${id}`, data),
    disable: (id, version, reason) => client.post(`/system/tenant/departments/${id}/disable`, { version, reason }),
    restore: (id, version, reason) => client.post(`/system/tenant/departments/${id}/restore`, { version, reason }),
    listMemberships: (id, params) => list(`/system/tenant/departments/${id}/memberships`, params),
    createMembership: (id, data) => client.post(`/system/tenant/departments/${id}/memberships`, data),
    updateMembership: (id, membershipId, data) => client.put(`/system/tenant/departments/${id}/memberships/${membershipId}`, data),
    closeMembership: (id, membershipId, version, reason) => client.post(`/system/tenant/departments/${id}/memberships/${membershipId}/close`, { version, reason })
  },
  projectGroups: {
    list: (params) => list('/system/tenant/project_groups', params),
    get: (id) => client.get(`/system/tenant/project_groups/${id}`),
    create: (data) => client.post('/system/tenant/project_groups', data),
    update: (id, data) => client.put(`/system/tenant/project_groups/${id}`, data),
    close: (id, version, reason) => client.post(`/system/tenant/project_groups/${id}/close`, { version, reason }),
    listMemberships: (id, params) => list(`/system/tenant/project_groups/${id}/memberships`, params),
    createMembership: (id, data) => client.post(`/system/tenant/project_groups/${id}/memberships`, data),
    updateMembership: (id, membershipId, data) => client.put(`/system/tenant/project_groups/${id}/memberships/${membershipId}`, data),
    closeMembership: (id, membershipId, version, reason) => client.post(`/system/tenant/project_groups/${id}/memberships/${membershipId}/close`, { version, reason })
  },
  oauthClients: {
    list: (params) => list('/system/tenant/oauth_clients', params),
    get: (clientId) => client.get(`/system/tenant/oauth_clients/${clientId}`),
    create: (data) => client.post('/system/tenant/oauth_clients', data),
    update: (clientId, data) => client.put(`/system/tenant/oauth_clients/${clientId}`, data),
    suspend: (clientId, version, reason) => client.post(`/system/tenant/oauth_clients/${clientId}/suspend`, { version, reason }),
    restore: (clientId, version, reason) => client.post(`/system/tenant/oauth_clients/${clientId}/restore`, { version, reason })
  },
  invitations: {
    list: (params) => list('/system/tenant/invitations', params),
    create: (email) => client.post('/system/tenant/invitations', { email }),
    revoke: (id) => client.post(`/system/tenant/invitations/${id}/revoke`)
  },
  tenantRoles: {
    list: () => list('/system/tenant/roles'),
    listAssignablePermissions: () => list('/system/tenant/role_permissions'),
    create: (data) => client.post('/system/tenant/roles', data),
    update: (id, data) => client.put(`/system/tenant/roles/${id}`, data),
    remove: (id, reason) => client.delete(`/system/tenant/roles/${id}`, { data: { reason } })
  },
  tenantRoleAssignments: {
    list: (params) => list('/system/tenant/role_assignments', params),
    create: (data) => client.post('/system/tenant/role_assignments', data),
    revoke: (id, reason) => client.post(`/system/tenant/role_assignments/${id}/revoke`, { reason })
  },
  audit: {
    list: (scope, params) => list(`/system/${scope}/audit/events`, params),
    summary: (scope, params) => client.get(`/system/${scope}/audit/events/summary`, { params }),
    trends: (scope, params) => client.get(`/system/${scope}/audit/events/trends`, { params }),
    export: (scope, params) => exportAudit(`/system/${scope}/audit/events/export`, params)
  }
}
