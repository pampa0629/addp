import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('tenant member selector ownership', () => {
  it('uses one IAM selector for role assignments and tenant audit', () => {
    const roleAssignments = source('../src/components/iam/TenantRoleAssignmentsPanel.vue')
    const audit = source('../src/components/iam/AuditPanel.vue')
    const selector = source('../src/components/iam/TenantMemberSelect.vue')

    expect(roleAssignments).toContain('<TenantMemberSelect')
    expect(audit).toContain('<TenantMemberSelect')
    expect(selector).toContain('<TenantMemberIdentity')
    expect(roleAssignments).not.toContain('v-for="member in membershipOptions"')
    expect(audit).not.toContain('v-for="member in membershipOptions"')
  })
})
