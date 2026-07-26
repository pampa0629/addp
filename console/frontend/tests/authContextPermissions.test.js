import { describe, expect, it } from 'vitest'

import { collectAuthContextPermissions } from '../../../common-frontend/basic/src/composables/useAuth'

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
