import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('tenant member selector ownership', () => {
  it('reuses the IAM identity renderer for grant recipients in history, revocation and inspection', () => {
    const grants = source('../src/components/engines/EngineSourceGrants.vue')
    const recipient = source('../src/components/engines/EngineGrantRecipient.vue')
    expect(grants.match(/<EngineGrantRecipient\b/g)).toHaveLength(3)
    expect(recipient).toContain('<TenantMemberIdentity')
    expect(recipient).toContain("from '../iam/TenantMemberIdentity.vue'")
    expect(grants).not.toContain('revokeRow?.recipient_id')
  })
  it('keeps user assignment and service-account assignment on distinct page entries', () => {
    const roleAssignments = source('../src/components/iam/TenantRoleAssignmentsPanel.vue')
    const audit = source('../src/components/iam/AuditPanel.vue')
    const serviceAccounts = source('../src/components/iam/ServicePrincipalAccountsPanel.vue')
    const userAccounts = source('../src/components/iam/TenantUserAccountsPanel.vue')
    const selector = source('../src/components/iam/TenantMemberSelect.vue')
    const engineDelegations = source('../src/components/engines/EngineAccessDelegations.vue')

    expect(roleAssignments).toContain('<TenantMemberSelect')
    expect(roleAssignments).toContain("principal_type: fixedMember.value?.principal_type || 'user'")
    expect(roleAssignments).toContain("iamAPI.memberships.listAll({ principal_type: 'user' })")
    expect(serviceAccounts).toContain('<TenantRoleAssignmentsPanel')
    expect(serviceAccounts).toContain(':fixed-membership="selectedAccount"')
    expect(userAccounts).toContain('<TenantRoleAssignmentsPanel')
    expect(userAccounts).toContain(':fixed-membership="selectedAccount"')
    expect(audit).toContain('<TenantMemberSelect')
    expect(selector).toContain('<TenantMemberIdentity')
    expect(roleAssignments).not.toContain('v-for="member in membershipOptions"')
    expect(audit).not.toContain('v-for="member in membershipOptions"')
    expect(engineDelegations).toContain('<TenantMemberSelect')
    expect(engineDelegations).not.toContain('v-for="member in members"')
  })
})
