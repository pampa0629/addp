import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

describe('organization management ownership', () => {
  it('requires manually entered codes for both organization forms and locks them after creation', () => {
    const departments = source('../src/components/iam/DepartmentsPanel.vue')
    const groups = source('../src/components/iam/ProjectGroupsPanel.vue')

    for (const panel of [departments, groups]) {
      expect(panel).not.toContain('suggestOrganizationCode')
      expect(panel).toContain('v-model="form.code"')
      expect(panel).toContain(':required="formMode === \'create\'"')
      expect(panel).toContain('system.iam.organization.codeCreateHint')
      expect(panel).toContain(':disabled="formMode === \'edit\'"')
    }
    expect(departments).toContain('code: form.code.trim()')
    expect(groups).not.toContain('starts_at')
    expect(groups).not.toContain('ends_at')
    expect(groups).not.toContain("'planned'")
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
