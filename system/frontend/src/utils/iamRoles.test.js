import { describe, expect, it, vi } from 'vitest'
import { createAuthStore, resolveRoleName, resolveRoleDescription } from '@common-ui'
import {
  buildTenantRoleOptions,
  hasTenantRole,
  isValidTenantRoleKey,
  needsTenantRoleSetup,
  partitionTenantRoleOptions,
  summarizeEffectiveTenantRoles,
  tenantRoleKeys
} from './iamRoles'

const messages = {
  'roles.tenant.administrator.name': '租户组织与权限管理员',
  'roles.tenant.administrator.description': '管理成员、组织与角色分配'
}
const t = (key) => messages[key]
const te = (key) => Object.hasOwn(messages, key)

describe('IAM tenant role presentation', () => {
  it('summarizes unique effective tenant roles and excludes inactive validity states', () => {
    const steward = { role_id: '3', role_key: 'tenant.data_steward', role_name: '数据管理员', effective_state: 'effective' }
    const administrator = { role_id: '1', role_key: 'tenant.administrator', role_name_i18n_key: 'roles.tenant.administrator.name', effective_state: 'effective' }
    const summary = summarizeEffectiveTenantRoles([
      { ...steward, scope_type: 'tenant' },
      administrator,
      { ...steward },
      ...['scheduled', 'expired', 'revoked'].map((effective_state, index) => ({ role_id: String(index + 10), role_key: 'tenant.data_viewer', status: 'active', effective_state }))
    ])
    expect(summary.map((role) => role.role_id)).toEqual(['1', '3'])
    expect(summary.map((role) => resolveRoleName(role, t, te))).toEqual(['租户组织与权限管理员', '数据管理员'])
    expect(summarizeEffectiveTenantRoles([])).toEqual([])
  })

  it('validates two lowercase role key segments and trims surrounding whitespace', () => {
    for (const key of ['custom.ontology_manager', 'tenant.reader', 'a.b', 'custom_2.role_3']) {
      expect(isValidTenantRoleKey(` \t${key}\n`), key).toBe(true)
    }
    for (const key of ['', 'ontology_manager', 'Custom.manager', 'custom.Manager', '1custom.manager', 'custom.1manager', 'custom.role-name', 'custom..manager', 'custom.role.extra', 'custom.本体', 'custom.role\nextra', 'custom .manager', null]) {
      expect(isValidTenantRoleKey(key), String(key)).toBe(false)
    }
  })

  it('resolves built-in role translations and keeps custom role names', () => {
    const builtIn = {
      role_key: 'tenant.administrator',
      name_i18n_key: 'roles.tenant.administrator.name',
      description_i18n_key: 'roles.tenant.administrator.description'
    }

    expect(resolveRoleName(builtIn, t, te)).toBe('租户组织与权限管理员')
    expect(resolveRoleDescription(builtIn, t, te)).toBe('管理成员、组织与角色分配')
    expect(resolveRoleName({ role_key: 'custom.reader', name: '研究数据只读' }, t, te)).toBe('研究数据只读')
  })

  it('assigns roles in the current tenant and filters account types', () => {
    const roles = [
      { id: '1', role_key: 'tenant.viewer', allowed_principal_types: ['user'], allowed_scope_types: ['tenant'] },
      { id: '2', role_key: 'tenant.engineer', allowed_principal_types: ['user'], allowed_scope_types: ['tenant'] },
      { id: '3', role_key: 'tenant.runtime', allowed_principal_types: ['service_principal'], allowed_scope_types: ['tenant'] }
    ]
    const assignments = [{ role_id: '1', status: 'active' }, { role_id: '2', status: 'revoked' }]
    expect(buildTenantRoleOptions(roles, assignments, { principalType: 'user' }).map(({ role_key, assigned }) => ({ role_key, assigned }))).toEqual([
      { role_key: 'tenant.engineer', assigned: false }, { role_key: 'tenant.viewer', assigned: true }
    ])
    expect(buildTenantRoleOptions(roles, assignments, { principalType: 'service_principal' }).map(role => role.id)).toEqual(['3'])
  })

  it('separates assignable roles from roles already active in the selected scope', () => {
    const groups = partitionTenantRoleOptions([
      { id: '1', role_key: 'tenant.viewer', assigned: false },
      { id: '2', role_key: 'tenant.operator', assigned: true },
      { id: '3', role_key: 'tenant.auditor', assigned: false }
    ])

    expect(groups.available.map((role) => role.role_key)).toEqual(['tenant.viewer', 'tenant.auditor'])
    expect(groups.assigned.map((role) => role.role_key)).toEqual(['tenant.operator'])
  })

  it('refreshes the access token before reloading authorization context', async () => {
    const values = new Map()
    vi.stubGlobal('localStorage', {
      getItem: (key) => values.get(key) || null,
      setItem: (key, value) => values.set(key, String(value)),
      removeItem: (key) => values.delete(key)
    })
    const config = createAuthStore('authorization-refresh-test', {})
    const calls = []
    const store = {
      refreshAccessToken: async (options) => calls.push(['refresh', options]),
      fetchAuthContext: async () => {
        calls.push(['context'])
        return { context: { type: 'tenant' } }
      }
    }

    await expect(config.actions.refreshAuthorization.call(store)).resolves.toEqual({ context: { type: 'tenant' } })
    expect(calls).toEqual([
      ['refresh', { force: true }],
      ['context']
    ])
    vi.unstubAllGlobals()
  })

  it('shows setup guidance only when tenant administrator is the sole role', () => {
    const authContext = {
      context: { type: 'tenant' },
      authorization: {
        role_assignments: [
          { role_key: 'tenant.administrator' },
          { role_key: 'tenant.administrator' }
        ]
      }
    }

    expect(tenantRoleKeys(authContext)).toEqual(['tenant.administrator'])
    expect(hasTenantRole(authContext, 'tenant.administrator')).toBe(true)
    expect(hasTenantRole(authContext, 'tenant.data_steward')).toBe(false)
    expect(needsTenantRoleSetup(authContext)).toBe(true)

    authContext.authorization.role_assignments.push({ role_key: 'tenant.data_steward' })
    expect(hasTenantRole(authContext, 'tenant.data_steward')).toBe(true)
    expect(needsTenantRoleSetup(authContext)).toBe(false)
    expect(needsTenantRoleSetup({ context: { type: 'platform' }, authorization: authContext.authorization })).toBe(false)
  })

  it('matches recommendation roles at the exact assignment scope', () => {
    const authContext = {
      context: { type: 'tenant' },
      authorization: {
        role_assignments: [
          { role_key: 'tenant.data_viewer', scope: { type: 'department', department_id: '8' } },
          { role_key: 'tenant.data_steward', scope: { type: 'tenant', tenant_id: '1' } }
        ]
      }
    }

    expect(hasTenantRole(authContext, 'tenant.data_viewer')).toBe(true)
    expect(hasTenantRole(authContext, 'tenant.data_viewer', 'tenant')).toBe(false)
    expect(hasTenantRole(authContext, 'tenant.data_viewer', 'department')).toBe(true)
    expect(hasTenantRole(authContext, 'tenant.data_steward', 'tenant')).toBe(true)
  })
})
