import { expect, test } from '@playwright/test'
const roles = ['system', 'security', 'audit']
async function mock(page, actor, conflict = false) {
  const writes = [], id = String(roles.indexOf(actor) + 1)
  const rows = roles.map((r, i) => ({ id: String(i + 10), version: '1', change_type: 'platform_identity_suspend', target_principal_id: String(i + 1), governance_role_key: `platform.${r}_administrator`, reviewer_role_key: `platform.${r === 'audit' ? 'system' : 'audit'}_administrator`, requested_by_principal_id: r === 'security' ? '1' : '2', status: 'pending', reason: `Change ${r}`, expires_at: new Date(Date.now() + 3600000).toISOString() }))
  await page.route('**/api/v1/system/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const reply = (json, status = 200) => route.fulfill({ json, status })
    if (path.endsWith('/refresh')) return reply({ access_token: 'governance-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id, display_name: actor })
    if (path.endsWith('/auth/context')) return reply({ principal: { id, type: 'user' }, context: { type: 'platform' }, authentication: { assurance_level: 'aal2' }, authorization: { role_assignments: [{ role_key: `platform.${actor}_administrator`, scope: { type: 'platform' }, permissions: ['iam.platform_administrator_change.read', ...(actor === 'audit' ? [] : ['iam.platform_administrator_change.create']), ...(actor === 'security' ? [] : ['iam.platform_administrator_change.approve', 'iam.platform_administrator_change.reject'])] }] } })
    if (path.endsWith('/administrator_changes/candidates')) return reply({ data: [...roles.map((r, i) => ({ id: String(i + 1), display_name: r, role_key: `platform.${r}_administrator`, mfa_enrolled: true, status: 'active' })), { id: '4', display_name: 'Successor', role_key: null, mfa_enrolled: true, status: 'active' }], total: 4 })
    if (path.endsWith('/administrator_changes') && req.method() === 'GET') return reply({ data: rows, total: rows.length })
    if (path.endsWith('/administrator_changes') && req.method() === 'POST') { writes.push(req.postDataJSON()); return reply({ id: '99', version: '1' }, 201) }
    const decision = path.match(/\/administrator_changes\/(\d+)\/(approve|reject|cancel)$/)
    if (decision) {
      writes.push({ id: decision[1], action: decision[2], ...req.postDataJSON() })
      if (conflict) return reply({ error: '变更请求状态已变化，请刷新' }, 409)
      const row = rows.find(r => r.id === decision[1]); row.status = 'applied'; row.version = '3'; return reply(row)
    }
    throw new Error(`Unexpected governance request ${req.method()} ${path}`)
  })
  return writes
}
for (const actor of roles) test(`governance actions follow the role matrix (${actor})`, async ({ page }) => {
  const writes = await mock(page, actor)
  await page.goto('/iam/accounts?tab=administrator-changes')
  await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible()
  await expect(page.getByRole('button', { name: '创建管理员变更请求' })).toHaveCount(actor === 'audit' ? 0 : 1)
  await expect(page.getByRole('button', { name: '批准', exact: true })).toHaveCount(actor === 'system' ? 1 : actor === 'audit' ? 2 : 0)
  if (actor === 'security') return
  await page.getByRole('button', { name: '批准', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('textarea').fill('Independent review')
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toHaveLength(1)
  expect(writes[0]).toMatchObject({ action: 'approve', version: '1', reason: 'Independent review' })
  expect(writes[0].id).not.toBe(String(roles.indexOf(actor) + 10))
})
test('handover submits the bound incumbent and successor without direct user mutations', async ({ page }) => {
  const writes = await mock(page, 'security')
  await page.goto('/iam/accounts?tab=administrator-changes')
  await page.getByRole('button', { name: '创建管理员变更请求' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('.el-select').nth(0).click(); await page.getByRole('option', { name: '交接管理员岗位', exact: true }).click()
  await dialog.locator('.el-select').nth(2).click(); await page.getByRole('option', { name: 'system (1)', exact: true }).click()
  await dialog.locator('.el-select').nth(3).click(); await page.getByRole('option', { name: 'Successor (4)', exact: true }).click()
  await dialog.locator('textarea').fill('Planned handover')
  await dialog.getByRole('button', { name: '提交', exact: true }).click(); await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ change_type: 'platform_role_replace', target_role_key: 'platform.system_administrator', target_principal_id: '1', successor_principal_id: '4', reason: 'Planned handover' }])
})
test('stale review preserves its reason without automatic retries', async ({ page }) => {
  const writes = await mock(page, 'system', true)
  await page.goto('/iam/accounts?tab=administrator-changes'); await page.getByRole('button', { name: '批准', exact: true }).click()
  const dialog = page.getByRole('dialog'); await dialog.locator('textarea').fill('Keep this reason')
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.getByText('变更请求状态已变化，请刷新', { exact: true })).toBeVisible()
  await expect(dialog.locator('textarea')).toHaveValue('Keep this reason'); expect(writes).toHaveLength(1)
})
test('public recovery consumes the proof in memory and never issues a session', async ({ page }) => {
  await page.setViewportSize({ width: 620, height: 900 })
  const writes = []
  await page.route('**/api/v1/system/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    if (path.endsWith('/refresh')) return route.fulfill({ status: 401, json: { error: 'No session' } })
    if (path.endsWith('/administrator-recovery/enrollments')) {
      expect(req.headers().authorization).toBeUndefined(); writes.push(req.postDataJSON())
      return route.fulfill({ status: 201, json: { enrollment_token: 'addp_pce_fixture', secret: 'JBSWY3DPEHPK3PXP', otpauth_uri: 'otpauth://totp/ADDP:fixture?secret=JBSWY3DPEHPK3PXP&issuer=ADDP', expires_at: new Date(Date.now() + 900000).toISOString() } })
    }
    if (path.endsWith('/administrator-recovery/completions')) { writes.push(req.postDataJSON()); return route.fulfill({ status: 204 }) }
    throw new Error(`Unexpected recovery request ${req.method()} ${path}`)
  })
  await page.goto('/administrator-recovery'); await page.locator('textarea').fill('addp_pcr_fixture')
  await page.getByRole('button', { name: '继续', exact: true }).click(); await expect(page.getByText('JBSWY3DPEHPK3PXP', { exact: true })).toBeVisible()
  await page.locator('input[autocomplete="new-password"]').nth(0).fill('new-administrator-password')
  await page.locator('input[autocomplete="new-password"]').nth(1).fill('new-administrator-password')
  await page.locator('input[autocomplete="one-time-code"]').fill('123456'); await page.getByRole('button', { name: '确认新凭据' }).click()
  await expect(page.getByText('凭据已更新，请重新登录。账号原有暂停状态保持不变。')).toBeVisible()
  await expect(page.getByText('JBSWY3DPEHPK3PXP', { exact: true })).toHaveCount(0)
  expect(writes).toEqual([{ recovery_token: 'addp_pcr_fixture' }, { enrollment_token: 'addp_pce_fixture', password: 'new-administrator-password', code: '123456' }])
  const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))
  for (const secret of ['addp_pcr_fixture', 'addp_pce_fixture', 'JBSWY3DPEHPK3PXP']) expect(storage).not.toContain(secret)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
})
