import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import en from '../i18n/en.json'
import zhCN from '../i18n/zh-cn.json'
import {
  accountSessionIsLoading,
  assuranceLevelKey,
  buildPermissionGroups,
  formatMemberOptionLabel,
  groupTenantMembers,
  groupPermissionsByNamespace,
  groupPermissionsByResource,
  permissionIdentity,
  permissionMatchesScopes,
  permissionResourceI18nKey,
  rolePrincipalTypeCounts,
  roleSupportsPrincipalType,
  resolveIAMModuleName,
  resolveMembershipSourceLabel
} from './iamPresentation'

describe('IAM presentation helpers', () => {
  it('localizes every built-in tenant role declared by the role catalog', () => {
    const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..')
    const catalog = readFileSync(resolve(repositoryRoot, 'system/authorization/builtin_roles.yaml'), 'utf8')
    const roleMessageKeys = [...catalog.matchAll(/^\s+(?:name|description)_i18n_key: (roles\.tenant\.[a-z0-9_]+\.(?:name|description))$/gm)]
      .map((match) => match[1])
    const hasMessage = (messages, key) => key.split('.').reduce((value, segment) => value?.[segment], messages) != null

    expect(roleMessageKeys.length).toBeGreaterThan(0)
    for (const [locale, messages] of [['zh-CN', zhCN], ['en', en]]) {
      expect(roleMessageKeys.filter((key) => !hasMessage(messages, key)), `${locale} built-in tenant role translations`).toEqual([])
    }
  })

  it('localizes every resource and action declared by permission manifests', () => {
    const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..')
    const permissionKeys = readdirSync(repositoryRoot, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .flatMap((entry) => {
        const manifestPath = resolve(repositoryRoot, entry.name, 'authorization/permissions.yaml')
        if (!existsSync(manifestPath)) return []
        return [...readFileSync(manifestPath, 'utf8').matchAll(/^  - key: ([a-z0-9_.]+)$/gm)]
          .map((match) => match[1])
      })

    const resources = [...new Set(permissionKeys.map((key) => key.split('.')[1]))].sort()
    const actions = [...new Set(permissionKeys.map((key) => key.split('.').at(-1)))].sort()

    for (const [locale, messages] of [['zh-CN', zhCN], ['en', en]]) {
      const roleMessages = messages.system.iam.roles
      expect(resources.filter((resource) => !roleMessages.resources[resource]), `${locale} resource translations`).toEqual([])
      expect(actions.filter((action) => !roleMessages.actions[action]), `${locale} action translations`).toEqual([])
    }
  })

  it('keeps account session presentation in loading state until both user and auth context arrive', () => {
    expect(accountSessionIsLoading({
      sessionStatus: 'initializing',
      isAuthenticated: false,
      user: null,
      authContext: null
    })).toBe(true)
    expect(accountSessionIsLoading({
      sessionStatus: 'authenticated',
      isAuthenticated: true,
      user: { id: '1' },
      authContext: null
    })).toBe(true)
    expect(accountSessionIsLoading({
      sessionStatus: 'authenticated',
      isAuthenticated: true,
      user: { id: '1' },
      authContext: { authentication: { assurance_level: 'aal2' } }
    })).toBe(false)
    expect(accountSessionIsLoading({
      sessionStatus: 'error',
      isAuthenticated: true,
      user: null,
      authContext: null
    })).toBe(false)
  })

  it('maps only supported assurance values to presentation keys', () => {
    expect(assuranceLevelKey('AAL2')).toBe('aal2')
    expect(assuranceLevelKey('')).toBe('unknown')
    expect(assuranceLevelKey('custom')).toBe('unknown')
  })

  it('marks the current tenant membership without guessing from its username', () => {
    const member = { id: '7', display_name: 'Alice', username: 'alice', principal_id: '3' }
    expect(formatMemberOptionLabel(member, '7', 'Current account')).toBe('Current account · Alice (alice)')
    expect(formatMemberOptionLabel(member, '8', 'Current account')).toBe('Alice (alice)')
  })

  it('groups current, user, and service accounts without mixing their types', () => {
    const members = [
      { id: 3, display_name: 'Worker', service_principal_name: 'addp-worker', principal_type: 'service_principal', status: 'active' },
      { id: 2, display_name: 'Bob', username: 'bob', principal_type: 'user', status: 'suspended' },
      { id: 1, display_name: 'Alice', username: 'alice', principal_type: 'user', status: 'active' }
    ]
    expect(groupTenantMembers(members, '1').map((group) => ({
      key: group.key,
      ids: group.members.map((member) => member.id)
    }))).toEqual([
      { key: 'current', ids: [1] },
      { key: 'user', ids: [2] },
      { key: 'service_principal', ids: [3] }
    ])
    expect(groupTenantMembers(members, '1', { activeOnly: true, principalType: 'user' }).map((group) => group.key)).toEqual(['current'])
  })

  it('includes the account type in searchable option labels', () => {
    const member = { id: '7', display_name: 'Alice', username: 'alice', principal_type: 'user' }
    expect(formatMemberOptionLabel(member, '7', 'Current account', 'User account')).toBe('Current account · User account · Alice (alice)')
  })

  it('groups permissions by stable namespace and sorts the result', () => {
    expect(groupPermissionsByNamespace(['meta.scan', 'catalog.read', 'meta.read'])).toEqual([
      { namespace: 'catalog', permissions: ['catalog.read'] },
      { namespace: 'meta', permissions: ['meta.read', 'meta.scan'] }
    ])
  })

  it('groups module permissions by resource and sorts actions with selection counts', () => {
    const permissions = [
      { permission_key: 'manager.data_item.update', action: 'update' },
      { permission_key: 'manager.connection.read', action: 'read' },
      { permission_key: 'manager.data_item.read', action: 'read' }
    ]

    expect(groupPermissionsByResource(permissions, ['manager.data_item.read'])).toEqual([
      {
        resource: 'connection',
        permissions: [permissions[1]],
        selectedCount: 0
      },
      {
        resource: 'data_item',
        permissions: [permissions[2], permissions[0]],
        selectedCount: 1
      }
    ])
  })

  it('uses localized module names and falls back to the stable identifier', () => {
    const t = (key) => ({ 'system.iam.modules.system': 'System Management' })[key] || key
    const te = (key) => key === 'system.iam.modules.system'
    expect(resolveIAMModuleName('system', t, te)).toBe('System Management')
    expect(resolveIAMModuleName('extension', t, te)).toBe('extension')
  })

  it('resolves only declared membership sources without constructing undefined i18n keys', () => {
    const messages = {
      'system.iam.source.manual': 'Manual',
      'system.iam.source.unknown': 'Unknown source'
    }
    const t = (key) => messages[key]
    const te = (key) => Object.hasOwn(messages, key)

    expect(resolveMembershipSourceLabel('manual', t, te)).toBe('Manual')
    expect(resolveMembershipSourceLabel(undefined, t, te)).toBe('Unknown source')
    expect(resolveMembershipSourceLabel('legacy', t, te)).toBe('Unknown source')
  })

  it('derives stable permission presentation fields from the permission key', () => {
    expect(permissionIdentity({ permission_key: 'manager.data_item.read' })).toEqual({
      permissionKey: 'manager.data_item.read',
      ownerModule: 'manager',
      resource: 'data_item',
      action: 'read'
    })
    expect(permissionResourceI18nKey({ permission_key: 'manager.data_item.read' }))
      .toBe('system.iam.roles.resources.data_item')
  })

  it('requires permissions to support every selected role scope', () => {
    const permission = { allowed_scope_types: ['tenant', 'department'] }
    expect(permissionMatchesScopes(permission, ['tenant'])).toBe(true)
    expect(permissionMatchesScopes(permission, ['tenant', 'project_group'])).toBe(false)
  })

  it('builds searchable permission groups with selected counts', () => {
    const permissions = [
      { permission_key: 'meta.scan_task.execute', owner_module: 'meta', action: 'execute', allowed_scope_types: ['tenant'] },
      { permission_key: 'manager.data_item.read', owner_module: 'manager', action: 'read', allowed_scope_types: ['tenant', 'department'] },
      { permission_key: 'manager.data_item.update', owner_module: 'manager', action: 'update', allowed_scope_types: ['tenant'] }
    ]
    expect(buildPermissionGroups(permissions, {
      search: 'Data Item',
      scopeTypes: ['tenant', 'department'],
      selectedKeys: ['manager.data_item.read'],
      getSearchValues: (permission) => permission.permission_key === 'manager.data_item.read' ? ['Data Item'] : []
    })).toEqual([{
      namespace: 'manager',
      permissions: [permissions[1]],
      selectedCount: 1
    }])
  })

  it('classifies roles by declared account type without inspecting role keys', () => {
    const roles = [
      { role_key: 'tenant.reader', allowed_principal_types: ['user'] },
      { role_key: 'custom-machine-role', allowed_principal_types: ['service_principal'] },
      { role_key: 'tenant.shared', allowed_principal_types: ['user', 'service_principal'] }
    ]

    expect(roleSupportsPrincipalType(roles[0], 'user')).toBe(true)
    expect(roleSupportsPrincipalType(roles[0], 'service_principal')).toBe(false)
    expect(roleSupportsPrincipalType(roles[1], 'service_principal')).toBe(true)
    expect(rolePrincipalTypeCounts(roles)).toEqual({
      all: 3,
      user: 2,
      service_principal: 2
    })
  })
})
