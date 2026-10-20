import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'

const permissions = ['system.engine.read', 'system.engine_access_delegation.read', 'system.engine_access_delegation.create',
  'system.engine_access_delegation.revoke', 'iam.tenant_membership.read']
async function fixture(page, { allowed = permissions, conflict = false, language = 'zh-cn' } = {}) {
  const writes = [], reads = []
  let row = { id: '3', display_name: '办理账号 B', status: 'active', effective_state: 'effective', version: 7,
    expires_at: '2099-01-01T00:00:00Z', grant_reason: 'Manage table authorization' }
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'delegation-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: allowed }] } })
    if (path.endsWith('/tenant/memberships')) {
      reads.push(path)
      return reply({ data: [{ id: '9007199254740993', principal_id: '8', principal_type: 'user', principal_status: 'active', status: 'active', display_name: '办理账号 B', username: 'handler-b' }], total: 1, total_pages: 1 })
    }
    if (path.includes('/access_delegations')) {
      if (request.method() === 'POST') {
        const body = request.postDataJSON(); writes.push({ path, body })
        if (conflict) return reply({ error: 'Version conflict' }, 409)
        if (path.endsWith('/revoke')) row = { ...row, status: 'revoked', effective_state: 'revoked', version: 8 }
        return reply(row, path.endsWith('/revoke') ? 200 : 201)
      }
      reads.push(path)
      return reply({ data: [row], total: 1, page: 1, page_size: 10, total_pages: 1 })
    }
    const engine = { id: '2', name: '授权验证 PostgreSQL', engine_type: 'postgresql', engine_origin: 'general', lifecycle_state: 'active', is_builtin: false, capabilities: { schema_version: 'engine.capabilities/v1', storage: {} } }
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines/2')) return reply(engine)
    if (path.endsWith('/engines')) return reply([engine])
    throw new Error(`Unexpected delegation fixture request: ${request.method()} ${path}`)
  })
  return { writes, reads }
}

test('delegation-only permission enters handlers from the list without exposing data authorization', async ({ page }) => {
  const { writes } = await fixture(page, { allowed: ['system.engine.read', 'system.engine_access_delegation.read'] })
  await page.goto('/engines')
  await page.getByTestId('engine-authorization-open').click()
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2?tab=delegations`)
  const dialog = page.getByRole('dialog', { name: '引擎授权 - 授权验证 PostgreSQL', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '委托授权', exact: true })).toBeVisible()
  await expect(dialog.getByRole('tab', { name: '数据授权', exact: true })).toHaveCount(0)
  await expect(dialog.getByRole('tab', { name: '基本信息', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('engine-access-delegations')).toContainText('办理账号 B')
  expect(writes).toEqual([])
})

test('delegation tab restores from URL, validates the draft, selects an account and submits only explicit fields', async ({ page }, testInfo) => {
  const { writes } = await fixture(page)
  await page.goto('/engines/2?tab=delegations')
  const panel = page.getByTestId('engine-access-delegations')
  await expect(panel).toBeVisible()
  await expect(page.getByTestId('delegation-form')).toHaveCount(0)
  await page.getByTestId('delegation-new').click()
  await expect(page.getByTestId('delegation-list')).toHaveCount(0)
  await page.getByTestId('delegation-create').click()
  await expect(panel).toContainText('请选择有效账号')
  expect(writes).toHaveLength(0)
  await panel.locator('.iam-member-select__member').click()
  await page.getByRole('option').filter({ hasText: 'handler-b' }).click()
  const date = panel.locator('.el-date-editor input')
  await date.fill('2098-01-01 00:00:00'); await date.press('Enter')
  await page.getByTestId('delegation-reason').fill('Delegate table handling')
  await page.screenshot({ path: testInfo.outputPath('delegation-filled-form.png') })
  await page.getByTestId('delegation-create').click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].path).toBe('/api/v1/system/engines/2/access_delegations')
  expect(writes[0].body).toEqual({ tenant_membership_id: '9007199254740993', expires_at: new Date(2098, 0, 1).toISOString(), reason: 'Delegate table handling' })
  await expect(panel).toContainText('管理委派已创建')
  await expect(page.getByTestId('delegation-form')).toHaveCount(0)
  await expect(page.getByTestId('delegation-list')).toBeVisible()
  await expect(page.getByTestId('delegation-workspace-tabs').getByRole('radio', { name: '已有委托', exact: true })).toBeChecked()
  await page.reload(); await expect(panel).toBeVisible()
  expect(writes).toHaveLength(1)
})

test('existing and new delegations are mutually exclusive, lazy and read-only on navigation', async ({ page }, testInfo) => {
  const { reads, writes } = await fixture(page)
  await page.goto('/engines/2?tab=delegations')
  const tabs = page.getByTestId('delegation-workspace-tabs')
  await expect(tabs.getByRole('radio', { name: '已有委托', exact: true })).toBeChecked()
  await expect(tabs.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(page.getByTestId('delegation-list')).toContainText('办理账号 B')
  await expect(page.getByTestId('delegation-form')).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/memberships'))).toBe(false)
  await page.screenshot({ path: testInfo.outputPath('delegation-existing.png'), animations: 'disabled' })
  await page.getByTestId('delegation-new').click()
  await expect(tabs.getByRole('radio', { checked: true })).toHaveCount(1)
  await expect(tabs.getByRole('radio', { name: '新增委托', exact: true })).toBeChecked()
  await expect(page.getByTestId('delegation-list')).toHaveCount(0)
  await expect(page.getByTestId('delegation-form')).toBeVisible()
  await page.getByTestId('delegation-reason').fill('Unsubmitted draft')
  await expect.poll(() => reads.filter(path => path.endsWith('/memberships')).length).toBe(1)
  await tabs.getByText('已有委托', { exact: true }).click()
  await expect(page.getByTestId('delegation-list')).toBeVisible()
  await expect(page.getByTestId('delegation-form')).toHaveCount(0)
  await page.getByTestId('delegation-new').click()
  await expect(page.getByTestId('delegation-reason')).toHaveValue('Unsubmitted draft')
  await page.getByTestId('delegation-cancel').click()
  await expect(tabs.getByRole('radio', { name: '已有委托', exact: true })).toBeFocused()
  await page.getByTestId('delegation-new').click()
  await expect(page.getByTestId('delegation-reason')).toHaveValue('')
  expect(writes).toEqual([])
})

async function fillDelegation(page) {
  const panel = page.getByTestId('delegation-form')
  await panel.locator('.iam-member-select__member').click()
  await page.getByRole('option').filter({ hasText: 'handler-b' }).click()
  const date = panel.locator('.el-date-editor input')
  await date.fill('2098-01-01 00:00:00'); await date.press('Enter')
  await page.getByTestId('delegation-reason').fill('Explicit delegation')
}
test('failed new delegation preserves the draft and does not retry on view changes', async ({ page }) => {
  const { writes } = await fixture(page, { conflict: true })
  await page.goto('/engines/2?tab=delegations'); await page.getByTestId('delegation-new').click(); await fillDelegation(page)
  await page.getByTestId('delegation-create').click()
  await expect(page.getByTestId('engine-access-delegations')).toContainText('Version conflict')
  await expect(page.getByTestId('delegation-reason')).toHaveValue('Explicit delegation')
  await page.getByTestId('delegation-workspace-tabs').getByText('已有委托', { exact: true }).click()
  await page.getByTestId('delegation-new').click()
  await expect(page.getByTestId('delegation-reason')).toHaveValue('Explicit delegation')
  expect(writes).toHaveLength(1)
})
test('create-only permission defaults to a form without acquiring list access', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: permissions.filter(permission => !['system.engine_access_delegation.read', 'system.engine_access_delegation.revoke'].includes(permission)) })
  await page.goto('/engines/2?tab=delegations')
  await expect(page.getByTestId('delegation-workspace-tabs').getByRole('radio', { name: '已有委托', exact: true })).toBeDisabled()
  await fillDelegation(page); await page.getByTestId('delegation-create').click()
  await expect(page.getByTestId('engine-access-delegations')).toContainText('管理委派已创建')
  await expect(page.getByTestId('delegation-form')).toBeVisible()
  await expect(page.getByTestId('delegation-reason')).toHaveValue('')
  await expect(page.getByTestId('delegation-list')).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/access_delegations'))).toBe(false)
  expect(writes).toHaveLength(1)
})
test('delegation workspace locks navigation during an explicit submission', async ({ page }) => {
  const { writes } = await fixture(page)
  await page.goto('/engines/2?tab=delegations'); await page.getByTestId('delegation-new').click(); await fillDelegation(page)
  let release, started
  const pending = new Promise(resolve => { release = resolve }), observed = new Promise(resolve => { started = resolve })
  await page.route('**/access_delegations', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    started(); await pending; await route.fallback()
  })
  try {
    await page.getByTestId('delegation-create').click(); await observed
    await expect(page.getByTestId('delegation-workspace-tabs').getByRole('radio', { name: '已有委托', exact: true })).toBeDisabled()
    await expect(page.getByTestId('delegation-cancel')).toBeDisabled()
    release()
    await expect(page.getByTestId('delegation-list')).toBeVisible()
    expect(writes).toHaveLength(1)
  } finally { release() }
})
test('English delegation workspace keeps a single selection and fits narrow windows', async ({ page }, testInfo) => {
  const { writes } = await fixture(page, { language: 'en' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/engines/2?tab=delegations'); await page.getByTestId('delegation-new').click()
  const tabs = page.getByTestId('delegation-workspace-tabs')
  await expect(tabs.getByRole('radio', { name: 'New delegation', exact: true })).toBeChecked()
  await expect(tabs.getByRole('radio', { checked: true })).toHaveCount(1)
  const bounds = await tabs.boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(390)
  await expect(page.getByTestId('delegation-list')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('delegation-new-mobile-en.png'), animations: 'disabled' })
  expect(writes).toEqual([])
})

test('new delegation without membership read permission cannot select or submit an account', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: permissions.filter(permission => permission !== 'iam.tenant_membership.read') })
  await page.goto('/engines/2?tab=delegations'); await page.getByTestId('delegation-new').click()
  await expect(page.getByTestId('delegation-form')).toContainText('选择账号需要租户成员读取权限')
  await expect(page.getByTestId('delegation-form').locator('.iam-member-select__member .el-select__wrapper')).toHaveClass(/is-disabled/)
  await expect(page.getByTestId('delegation-create')).toBeDisabled()
  expect(reads.some(path => path.endsWith('/memberships'))).toBe(false)
  expect(writes).toEqual([])
})

test('read-only access makes no membership request and offers no write control', async ({ page }) => {
  const { reads, writes } = await fixture(page, { allowed: ['system.engine.read', 'system.engine_access_delegation.read'] })
  await page.goto('/engines/2?tab=delegations')
  await expect(page.getByTestId('engine-access-delegations')).toContainText('办理账号 B')
  await expect(page.getByTestId('delegation-create')).toHaveCount(0)
  await expect(page.getByTestId('delegation-revoke')).toHaveCount(0)
  expect(reads.some(path => path.endsWith('/memberships'))).toBe(false)
  expect(writes).toEqual([])
})

test('revocation sends the observed version and explicit reason, then refreshes without granting access', async ({ page }) => {
  const { writes } = await fixture(page)
  await page.goto('/engines/2?tab=delegations')
  await page.getByTestId('delegation-revoke').click()
  await page.getByTestId('delegation-revoke-reason').fill('Handover finished')
  await page.getByTestId('delegation-revoke-confirm').click()
  await expect(page.getByTestId('engine-access-delegations')).toContainText('已撤销')
  expect(writes).toEqual([{ path: '/api/v1/system/engines/2/access_delegations/3/revoke', body: { version: 7, reason: 'Handover finished' } }])
})

test('a revocation conflict retains the reason and never automatically retries', async ({ page }) => {
  const { writes } = await fixture(page, { conflict: true })
  await page.goto('/engines/2?tab=delegations')
  await page.getByTestId('delegation-revoke').click()
  await page.getByTestId('delegation-revoke-reason').fill('My reason')
  await page.getByTestId('delegation-revoke-confirm').click()
  await expect(page.getByTestId('delegation-revoke-reason')).toHaveValue('My reason')
  await expect(page.getByRole('dialog').last()).toContainText('Version conflict')
  expect(writes).toHaveLength(1)
})

test('without delegation permissions an invalid delegation tab canonicalizes to basic details', async ({ page }) => {
  const { reads } = await fixture(page, { allowed: ['system.engine.read'] })
  await page.goto('/engines/2?tab=delegations')
  await expect(page).toHaveURL(`${browserTestOrigin('system')}/engines/2`)
  await expect(page.getByTestId('engine-access-delegations')).toHaveCount(0)
  await expect(page.getByTestId('engine-authorization-open')).toHaveCount(0)
  expect(reads).toEqual([])
})
