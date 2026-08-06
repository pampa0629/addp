import { describe, expect, it, vi } from 'vitest'
import { createAuthStore } from '@common-ui'
import {
  buildTenantRoleOptions,
  formatTenantAssignmentScope,
  hasTenantRole,
  needsTenantRoleSetup,
  resolveRoleDescription,
  resolveRoleName,
  resolveTenantScopeLabel,
  tenantRoleKeys
} from './iamRoles'

const messages = {
  'roles.tenant.administrator.name': '租户组织与权限管理员',
  'roles.tenant.administrator.description': '管理成员、组织与角色分配'
}
const t = (key) => messages[key]
const te = (key) => Object.hasOwn(messages, key)

describe('IAM tenant role presentation', () => {
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

  it('filters by scope and sorts exact assignments behind selectable roles', () => {
    const roles = [
      { id: '1', role_key: 'tenant.viewer', allowed_principal_types: ['user'], allowed_scope_types: ['tenant', 'department'] },
      { id: '2', role_key: 'tenant.engineer', allowed_principal_types: ['user'], allowed_scope_types: ['tenant'] },
      { id: '3', role_key: 'tenant.department_lead', allowed_principal_types: ['user'], allowed_scope_types: ['department'] },
      { id: '4', role_key: 'tenant.asset_runtime', allowed_principal_types: ['service_principal'], allowed_scope_types: ['tenant'] }
    ]
    const assignments = [
      { role_id: '1', status: 'active', scope_type: 'tenant', department_id: null, project_group_id: null },
      { role_id: '3', status: 'active', scope_type: 'department', department_id: '8', project_group_id: null }
    ]

    expect(buildTenantRoleOptions(roles, assignments, {
      principalType: 'user', scopeType: 'tenant', departmentId: '', projectGroupId: ''
    }).map(({ role_key, assigned, assignedElsewhere }) => ({ role_key, assigned, assignedElsewhere }))).toEqual([
      { role_key: 'tenant.engineer', assigned: false, assignedElsewhere: false },
      { role_key: 'tenant.viewer', assigned: true, assignedElsewhere: false }
    ])

    expect(buildTenantRoleOptions(roles, assignments, {
      principalType: 'user', scopeType: 'department', departmentId: '9', projectGroupId: ''
    }).map(({ role_key, assigned, assignedElsewhere }) => ({ role_key, assigned, assignedElsewhere }))).toEqual([
      { role_key: 'tenant.department_lead', assigned: false, assignedElsewhere: true },
      { role_key: 'tenant.viewer', assigned: false, assignedElsewhere: true }
    ])

    expect(buildTenantRoleOptions(roles, assignments, {
      principalType: 'service_principal', scopeType: 'tenant', departmentId: '', projectGroupId: ''
    }).map((role) => role.role_key)).toEqual(['tenant.asset_runtime'])
  })

  it('formats assignment scopes without constructing invalid i18n keys', () => {
    const translate = vi.fn((key) => ({
      'system.iam.roles.scope.tenant': '租户',
      'system.iam.roles.scope.department': '部门'
    })[key])

    expect(formatTenantAssignmentScope({ scope_type: 'tenant' }, translate)).toBe('租户')
    expect(formatTenantAssignmentScope({ scope_type: 'department', department_id: '8' }, translate)).toBe('部门 #8')
    expect(formatTenantAssignmentScope({}, translate)).toBe('-')
    expect(resolveTenantScopeLabel('custom_scope', translate)).toBe('custom_scope')
    expect(translate).toHaveBeenCalledTimes(2)
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
