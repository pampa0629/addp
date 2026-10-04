import { describe, expect, it } from 'vitest'

import { collectAuthContextPermissions, createAuthAPI } from '../../../common-frontend/basic/src/composables/useAuth'

describe('collectAuthContextPermissions', () => {
  it('collects a stable unique permission set across assignments', () => {
    const authContext = {
      context: { type: 'platform' },
      authorization: {
        role_assignments: [
          { role_key: 'platform.system_administrator', scope: { type: 'platform' }, permissions: ['platform.tenant.read', 'iam.platform_identity_change.read'] },
          { role_key: 'platform.statistics_viewer', scope: { type: 'platform' }, permissions: ['statistics.summary.read', 'platform.tenant.read'] }
        ]
      }
    }

    expect(collectAuthContextPermissions(authContext)).toEqual([
      'iam.platform_identity_change.read',
      'platform.tenant.read',
      'statistics.summary.read'
    ])
  })

  it('defaults to an empty permission set for missing authorization facts', () => {
    expect(collectAuthContextPermissions(null)).toEqual([])
    expect(collectAuthContextPermissions({ authorization: { role_assignments: [] } })).toEqual([])
  })
})

describe('tenant navigation permissions', () => {
  it('ignores organizational and other tenant assignments', () => {
    const authContext = {
      context: { type: 'tenant', tenant_id: '3' },
      authorization: { role_assignments: [
        { scope: { type: 'department', tenant_id: '3', department_id: '9' }, permissions: ['transfer.task.read'] },
        { scope: { type: 'tenant', tenant_id: '3' }, permissions: ['transfer.task.create', 'meta.catalog.read'] },
        { scope: { type: 'tenant', tenant_id: '4' }, permissions: ['transfer.task.delete'] }
      ] }
    }
    expect(collectAuthContextPermissions(authContext)).toEqual(['meta.catalog.read', 'transfer.task.create'])
    expect(collectAuthContextPermissions({ ...authContext, context: { type: 'platform' } })).toEqual([])
  })
})

describe('browser context API contract', () => {
  it('uses the current Bearer token and only submits the canonical context choice', async () => {
    const calls = []
    const client = {
      get: async (...args) => {
        calls.push(['get', ...args])
        return { data: { contexts: [] } }
      },
      post: async (...args) => {
        calls.push(['post', ...args])
        return { data: { access_token: 'addp_at_new', expires_in: 900 } }
      }
    }
    const api = createAuthAPI(client)

    await api.getContextOptions('addp_at_current')
    await api.logout('addp_at_current')
    await api.switchContext('addp_at_current', {
      type: 'tenant',
      tenant_membership_id: '18',
      tenant_id: 'must-not-be-forwarded'
    })

    expect(calls).toEqual([
      ['get', '/auth/context-options', { headers: { Authorization: 'Bearer addp_at_current' } }],
      ['post', '/logout', null, { headers: { Authorization: 'Bearer addp_at_current' }, withCredentials: true }],
      ['post', '/auth/context-switches', {
        context_type: 'tenant',
        tenant_membership_id: '18'
      }, {
        headers: { Authorization: 'Bearer addp_at_current' },
        withCredentials: true
      }]
    ])
  })
})
