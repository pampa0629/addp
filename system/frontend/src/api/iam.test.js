import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('./client', () => ({ default: client }))

import { iamAPI } from './iam'

describe('IAM management API contract', () => {
  beforeEach(() => vi.clearAllMocks())

  it('creates and initializes tenants through the platform tenant path', () => {
    iamAPI.platformTenants.listAdministratorCandidates({ search: 'alice' })
    iamAPI.platformTenants.create({
      code: 'research',
      name: 'Research',
      initial_administrator_principal_id: '42'
    })
    iamAPI.platformTenants.initialize('1', '42')

    expect(client.get).toHaveBeenCalledWith('/system/platform/tenant_administrator_candidates', {
      params: { search: 'alice' }
    })
    expect(client.post).toHaveBeenNthCalledWith(1, '/system/platform/tenants', {
      code: 'research',
      name: 'Research',
      initial_administrator_principal_id: '42'
    })
    expect(client.post).toHaveBeenNthCalledWith(2, '/system/platform/tenants/1/initialization', {
      initial_administrator_principal_id: '42'
    })
  })

  it('uses the single tenant role and assignment API family', () => {
    const role = { role_key: 'custom.reader', scope_types: ['tenant'], permission_keys: ['asset.catalog.read'] }
    iamAPI.tenantRoles.list()
    iamAPI.tenantRoles.listAssignablePermissions()
    iamAPI.tenantRoles.create(role)
    iamAPI.tenantRoles.update('7', role)
    iamAPI.tenantRoles.remove('7', 'retired')
    iamAPI.tenantRoleAssignments.list({ membership_id: '9', status: 'active', scope_type: 'tenant' })
    iamAPI.tenantRoleAssignments.create({ membership_id: '9', role_id: '7', scope_type: 'tenant' })
    iamAPI.tenantRoleAssignments.revoke('11', 'rotation')

    expect(client.get).toHaveBeenNthCalledWith(1, '/system/tenant/roles', { params: undefined })
    expect(client.get).toHaveBeenNthCalledWith(2, '/system/tenant/role_permissions', { params: undefined })
    expect(client.post).toHaveBeenCalledWith('/system/tenant/roles', role)
    expect(client.put).toHaveBeenCalledWith('/system/tenant/roles/7', role)
    expect(client.delete).toHaveBeenCalledWith('/system/tenant/roles/7', { data: { reason: 'retired' } })
    expect(client.get).toHaveBeenNthCalledWith(3, '/system/tenant/role_assignments', {
      params: { membership_id: '9', status: 'active', scope_type: 'tenant' }
    })
    expect(client.post).toHaveBeenCalledWith('/system/tenant/role_assignments', {
      membership_id: '9', role_id: '7', scope_type: 'tenant'
    })
    expect(client.post).toHaveBeenCalledWith('/system/tenant/role_assignments/11/revoke', { reason: 'rotation' })
  })

  it('resets an ordinary local account through the governed platform path', () => {
    const request = { new_password: 'new-secret', reason: 'user lost password' }

    iamAPI.platformUsers.resetPassword('4', request)

    expect(client.post).toHaveBeenCalledWith('/system/platform/users/4/reset-password', request)
  })

  it('resets an ordinary user authenticator through the governed platform path', () => {
    const request = { reason: 'user lost authenticator' }

    iamAPI.platformUsers.resetMFA('4', request)

    expect(client.post).toHaveBeenCalledWith('/system/platform/users/4/reset-mfa', request)
  })

  it('uses the single authenticated MFA enrollment and step-up path', () => {
    iamAPI.mfa.status()
    iamAPI.mfa.beginEnrollment('current-password')
    iamAPI.mfa.completeEnrollment('addp_mfe_token', '123456')
    iamAPI.mfa.beginStepUp()
    iamAPI.mfa.completeStepUp('addp_mfc_token', '654321')

    expect(client.get).toHaveBeenCalledWith('/system/auth/mfa')
    expect(client.post).toHaveBeenNthCalledWith(1, '/system/auth/mfa/totp-enrollments', {
      current_password: 'current-password'
    }, { withCredentials: true })
    expect(client.post).toHaveBeenNthCalledWith(2, '/system/auth/mfa/totp-enrollment-verifications', {
      enrollment_token: 'addp_mfe_token', code: '123456'
    }, { withCredentials: true })
    expect(client.post).toHaveBeenNthCalledWith(3, '/system/auth/mfa/step-up-challenges', null, { withCredentials: true })
    expect(client.post).toHaveBeenNthCalledWith(4, '/system/auth/mfa/step-up-verifications', {
      challenge_token: 'addp_mfc_token', code: '654321'
    }, { withCredentials: true })
  })
})
