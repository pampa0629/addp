import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('tenant member selector ownership', () => {
  it('keeps user assignment and service-account assignment on distinct page entries', () => {
    const roleAssignments = source('../src/components/iam/TenantRoleAssignmentsPanel.vue')
    const audit = source('../src/components/iam/AuditPanel.vue')
    const serviceAccounts = source('../src/components/iam/ServicePrincipalAccountsPanel.vue')
    const selector = source('../src/components/iam/TenantMemberSelect.vue')

    expect(roleAssignments).toContain('<TenantMemberSelect')
    expect(roleAssignments).toContain("principal_type: fixedMembership.value ? 'service_principal' : 'user'")
    expect(roleAssignments).toContain("iamAPI.memberships.listAll({ principal_type: 'user' })")
    expect(serviceAccounts).toContain('<TenantRoleAssignmentsPanel')
    expect(serviceAccounts).toContain(':fixed-membership="selectedAccount"')
    expect(audit).toContain('<TenantMemberSelect')
    expect(selector).toContain('<TenantMemberIdentity')
    expect(roleAssignments).not.toContain('v-for="member in membershipOptions"')
    expect(audit).not.toContain('v-for="member in membershipOptions"')
  })
})
