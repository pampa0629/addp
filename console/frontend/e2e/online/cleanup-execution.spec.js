import { expect, request, test } from '@playwright/test'
import { readFileSync, writeFileSync } from 'node:fs'
import { identity, json, login } from './transfer-browser-support.js'

test.use({ screenshot: 'off', trace: 'off', video: 'off' })

test('Cleanup revocation clears refresh and pagination views before late real results return', async ({ browser }) => {
  test.setTimeout(180_000)
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const input = JSON.parse(readFileSync(process.env.ADDP_CLEANUP_BROWSER_INPUT, 'utf8'))
  const report = { schema_version: 'addp.cleanup-execution-browser/v1', result: 'failed',
    run_id: input.run_id, tenant_id: input.tenant_id, principal_id: input.principal_id,
    request_id: input.request_id, execution_id: input.execution_id, phases: [] }
  const contexts = [], holds = [], deliveries = []
  let stage = 'login', release
  const released = new Promise(resolve => { release = resolve })
  const historyPath = '/api/v1/system/admin/cleanup/history'
  const resultPath = `/api/v1/asset/cleanup-executions/${input.execution_id}/result`
  const assignmentPath = `/api/v1/system/tenant/role_assignments/${input.assignment_id}`
  const pages = []
  let controller
  try {
    expect(input.permissions).toHaveLength(12)
    expect(input.assignment_id).toMatch(/^[1-9][0-9]*$/)
    for (const phase of ['refresh', 'pagination']) {
      stage = phase + '_login'
      const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
      contexts.push(context)
      const page = await context.newPage()
      await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
      const token = await login(page, process.env.ADDP_CLEANUP_BROWSER_USERNAME, process.env.ADDP_CLEANUP_BROWSER_PASSWORD, '/system/cleanup')
      const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
      try {
        const auth = await json(await api.get('/api/v1/system/auth/context'), 'normal browser identity')
        const current = identity(auth)
        expect(current.principalType).toBe('user'); expect(current.contextType).toBe('tenant')
        expect(current.principalID).toBe(input.principal_id); expect(current.tenantID).toBe(input.tenant_id)
        expect([...current.permissions].sort()).toEqual([...input.permissions].sort())
        expect(String(auth.context.tenant_membership_id)).toBe(input.membership_id)
      } finally { await api.dispose() }
      const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
      await expect(frame.locator('.cleanup-module-results')).toContainText(input.execution_id)
      // Exercise a real second page before revocation; no fabricated total or rows.
      stage = phase + '_authorized_pagination'
      const next = page.waitForResponse(r => new URL(r.url()).pathname === historyPath && new URL(r.url()).searchParams.get('page') === '2')
      await frame.locator('.el-pagination .btn-next').click()
      const second = await json(await next, 'second Cleanup history page')
      expect(second.total).toBeGreaterThan(10); expect(second.tasks.length).toBeGreaterThan(0)
      const previous = page.waitForResponse(r => new URL(r.url()).pathname === historyPath && new URL(r.url()).searchParams.get('page') === '1')
      await frame.locator('.el-pagination .btn-prev').click()
      expect((await previous).status()).toBe(200)
      await expect(frame.locator('.cleanup-module-results')).toContainText(input.execution_id)
      stage = phase + '_hold_real_result'
      await page.route(`**${resultPath}`, async route => {
        const response = await route.fetch()
        expect(response.status()).toBe(200)
        const value = await response.json()
        expect(value.schema).toBe('addp.cleanup-professional-result/v1')
        expect(value.request_id).toBe(input.request_id); expect(value.execution_id).toBe(input.execution_id)
        expect(value.module).toBe('asset'); expect(value.status).toBe('success')
        holds.push(phase)
        await released
        try {
          await route.fulfill({ response })
          deliveries.push(phase)
        } catch {
          if (route.request().failure()?.errorText === 'net::ERR_ABORTED') deliveries.push(phase)
          else deliveries.push('unexpected_delivery_failure')
        }
      })
      await frame.locator('.cleanup-module-results').getByRole('button', { name: '模块专业结果', exact: true }).click()
      await expect.poll(() => holds.includes(phase)).toBe(true)
      pages.push({ page, frame, phase })
    }
    stage = 'exact_role_revocation'
    controller = await request.newContext({ baseURL: process.env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${process.env.ADDP_CLEANUP_BROWSER_IAM_TOKEN}` } })
    const auth = await json(await controller.get('/api/v1/system/auth/context'), 'revocation controller identity')
    const admin = identity(auth)
    expect(admin.principalType).toBe('user'); expect(admin.contextType).toBe('tenant')
    expect(admin.tenantID).toBe(input.tenant_id); expect(admin.principalID).not.toBe(input.principal_id)
    expect(auth.authorization.role_assignments.map(x => x.role_key)).toContain('tenant.administrator')
    for (const permission of ['iam.tenant_role_assignment.read', 'iam.tenant_role_assignment.revoke']) expect(admin.permissions.has(permission)).toBe(true)
    const exactAssignment = value => {
      expect(value).toMatchObject({ id: input.assignment_id, principal_id: input.principal_id,
        membership_id: input.membership_id, principal_type: 'user', username: 'external-online-cleanup-other',
        role_key: 'online.external_online_cleanup_other', scope_type: 'tenant' })
    }
    const before = await json(await controller.get(assignmentPath), 'exact role assignment')
    exactAssignment(before); expect(before.status).toBe('active')
    const revoked = await json(await controller.post(assignmentPath + '/revoke',
      { data: { reason: 'Disposable Cleanup browser authorization acceptance' } }), 'exact role revocation')
    exactAssignment(revoked); expect(revoked.status).toBe('revoked')
    for (const { page, frame, phase } of pages) {
      stage = phase + '_denied_read'
      const denied = page.waitForResponse(r => new URL(r.url()).pathname === historyPath && r.request().method() === 'GET')
      if (phase === 'refresh') await frame.getByRole('button', { name: '刷新历史', exact: true }).click()
      else await frame.locator('.el-pagination .btn-next').click()
      expect((await denied).status()).toBe(401)
      await expect(frame.locator('.cleanup-module-results')).toHaveCount(0)
      await expect(frame.locator('.el-table__row')).toHaveCount(0)
      await expect(frame.locator('.cleanup-professional-result')).toHaveCount(0)
    }
    stage = 'release_late_real_responses'
    release()
    await expect.poll(() => deliveries.length).toBe(2)
    expect(deliveries.sort()).toEqual(['pagination', 'refresh'])
    for (const { frame, phase } of pages) {
      await expect(frame.locator('.cleanup-module-results')).toHaveCount(0)
      await expect(frame.locator('.el-table__row')).toHaveCount(0)
      await expect(frame.locator('.cleanup-professional-result')).toHaveCount(0)
      report.phases.push({ phase, real_pagination: true, result_read_before_revoke: true,
        real_denial: true, previous_view_cleared: true, late_result_rejected: true })
    }
    report.result = 'passed'
  } catch {
    report.failure_stage = stage
    throw new Error('Cleanup browser failed at stage ' + stage)
  } finally {
    release()
    for (const context of contexts) await context.close()
    if (controller) await controller.dispose()
    writeFileSync(process.env.ADDP_CLEANUP_BROWSER_REPORT, JSON.stringify(report), { mode: 0o600, flag: 'wx' })
  }
})
