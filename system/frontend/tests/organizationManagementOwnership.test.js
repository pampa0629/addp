import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('organization management ownership', () => {
  it('lets System own department codes instead of asking administrators to enter them', () => {
    const departments = source('../src/components/iam/DepartmentsPanel.vue')

    expect(departments).not.toContain('v-model="form.code"')
    expect(departments).toContain("iamAPI.departments.create({ name: form.name.trim(), parent_id: form.parentId || null })")
  })

  it('limits organization member candidates to user accounts and guards empty table rows', () => {
    const memberships = source('../src/components/iam/OrganizationMembershipsDialog.vue')

    expect(memberships).toContain("principal_type: 'user'")
    expect(memberships).toContain("filter(candidate => candidate.principal_type === 'user')")
    expect(memberships).toContain('organizationLabel(row.membership_type)')
    expect(memberships).toContain('organizationLabel(relationRole(row))')
    expect(memberships).not.toContain('system.iam.organization.${row.membership_type}')
  })
})
