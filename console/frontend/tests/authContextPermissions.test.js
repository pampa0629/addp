import { describe, expect, it } from 'vitest'

import { collectAuthContextPermissions, createAuthAPI } from '../../../common-frontend/basic/src/composables/useAuth'

describe('collectAuthContextPermissions', () => {
  it('collects a stable unique permission set across assignments', () => {
    const authContext = {
      authorization: {
        role_assignments: [
          { role_key: 'platform.system_administrator', permissions: ['platform.tenant.read', 'iam.platform_identity_change.read'] },
          { role_key: 'platform.statistics_viewer', permissions: ['statistics.summary.read', 'platform.tenant.read'] }
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
    await api.switchContext('addp_at_current', {
      type: 'tenant',
      tenant_membership_id: '18',
      tenant_id: 'must-not-be-forwarded'
    })

    expect(calls).toEqual([
      ['get', '/auth/context-options', { headers: { Authorization: 'Bearer addp_at_current' } }],
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
