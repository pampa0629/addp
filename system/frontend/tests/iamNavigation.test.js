import { describe, expect, it } from 'vitest'
import { availableIAMPages, availableIAMTabs } from '../src/config/iamNavigation'

function permissionChecker(granted) {
  const permissions = new Set(granted)
  return permission => permissions.has(permission)
}

describe('System IAM information architecture', () => {
  it('groups platform management objects into business categories', () => {
    const can = permissionChecker([
      'iam.user.read',
      'iam.platform_identity_change.read',
      'platform.tenant.read',
      'iam.security_policy.read',
      'audit.event.read'
    ])

    expect(availableIAMPages('platform', can).map(page => page.key)).toEqual([
      'identity',
      'organization',
      'security'
    ])
    expect(availableIAMTabs('identity', 'platform', can).map(tab => tab.key)).toEqual([
      'users',
      'identity-changes'
    ])
    expect(availableIAMTabs('security', 'platform', can).map(tab => tab.key)).toEqual([
      'account-security',
      'security-policy',
      'audit'
    ])
  })

  it('groups tenant management objects into the four agreed categories', () => {
    const can = permissionChecker([
      'iam.tenant_membership.read',
      'iam.tenant_invitation.read',
      'iam.department.read',
      'iam.project_group.read',
      'iam.tenant_role.read',
      'iam.tenant_role_assignment.read',
      'iam.oauth_client.read',
      'audit.tenant_event.read'
    ])

    expect(availableIAMPages('tenant', can).map(page => page.key)).toEqual([
      'identity',
      'organization',
      'access',
      'security'
    ])
    expect(availableIAMTabs('organization', 'tenant', can).map(tab => tab.key)).toEqual([
      'departments',
      'project-groups'
    ])
    expect(availableIAMTabs('access', 'tenant', can).map(tab => tab.key)).toEqual([
      'roles',
      'role-assignments',
      'oauth-clients'
    ])
  })

  it('keeps account security available without exposing unauthorized categories', () => {
    const can = permissionChecker([])
    expect(availableIAMPages('tenant', can).map(page => page.key)).toEqual(['security'])
    expect(availableIAMTabs('security', 'tenant', can).map(tab => tab.key)).toEqual(['account-security'])
  })
})
