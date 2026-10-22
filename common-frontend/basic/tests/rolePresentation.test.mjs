import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolveRoleName, resolveRoleDescription } from '../src/utils/rolePresentation.mjs'

test('shared role names have one translation and implementation owner', () => {
  for (const locale of ['zh-cn', 'en']) {
    const shared = JSON.parse(readFileSync(new URL(`../src/i18n/${locale}.json`, import.meta.url)))
    const system = JSON.parse(readFileSync(new URL(`../../../system/frontend/src/i18n/${locale}.json`, import.meta.url)))
    assert.ok(shared.roles.tenant.administrator.name)
    assert.equal(system.roles, undefined)
  }
  const source = readFileSync(new URL('../../../system/frontend/src/utils/iamRoles.js', import.meta.url), 'utf8')
  assert.ok(!source.includes('function resolveRoleName'))
  assert.ok(!source.includes('function resolveRoleDescription'))
  assert.ok(!source.includes('resolveRoleName'))
  const messages = { 'roles.tenant.reader.name': 'Reader', 'roles.tenant.reader.description': 'Read data' }
  const t = key => messages[key]
  const te = key => Object.hasOwn(messages, key)
  assert.equal(resolveRoleName({ name: '统计人员' }, t, te), '统计人员')
  assert.equal(resolveRoleName({ name_i18n_key: 'roles.tenant.reader.name' }, t, te), 'Reader')
  assert.equal(resolveRoleName({ role_name: 'Reviewer' }, t, te), 'Reviewer')
  assert.equal(resolveRoleDescription({ role_key: 'tenant.reader' }, t, te), 'Read data')
})
