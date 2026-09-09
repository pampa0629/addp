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
      'organization',
      'accounts',
      'security'
    ])
    expect(availableIAMTabs('accounts', 'platform', can).map(tab => tab.key)).toEqual([
      'users',
      'identity-changes'
    ])
    expect(availableIAMTabs('security', 'platform', can).map(tab => tab.key)).toEqual([
      'security-policy',
      'audit'
    ])
  })

  it('groups tenant management objects into the five agreed categories', () => {
    const can = permissionChecker([
      'iam.tenant_membership.read',
      'iam.tenant_invitation.read',
      'iam.department.read',
      'iam.project_group.read',
      'iam.tenant_role.read',
      'iam.tenant_role_assignment.read',
      'iam.service_account.read',
      'iam.api_consumer.read',
      'iam.oauth_client.read',
      'audit.tenant_event.read'
    ])

    expect(availableIAMPages('tenant', can).map(page => page.key)).toEqual([
      'organization',
      'accounts',
      'roles',
      'application-access',
      'security'
    ])
    expect(availableIAMTabs('organization', 'tenant', can).map(tab => tab.key)).toEqual([
      'departments',
      'project-groups'
    ])
    expect(availableIAMTabs('accounts', 'tenant', can).map(tab => tab.key)).toEqual([
      'user-accounts',
      'invitations'
    ])
    expect(availableIAMTabs('roles', 'tenant', can).map(tab => tab.key)).toEqual([
      'role-definitions',
      'role-assignments'
    ])
    expect(availableIAMTabs('application-access', 'tenant', can).map(tab => tab.key)).toEqual([
      'service-accounts',
      'api-consumers',
      'oauth-clients'
    ])
  })

  it('does not expose IAM management categories without permission', () => {
    const can = permissionChecker([])
    expect(availableIAMPages('tenant', can)).toEqual([])
    expect(availableIAMTabs('accounts', 'tenant', can)).toEqual([])
  })
})
