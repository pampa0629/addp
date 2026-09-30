import { expect, test } from '@playwright/test'

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status, contentType: 'application/json',
    headers: {
      'access-control-allow-origin': 'http://127.0.0.1:4173',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,PUT,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

for (const access of ['full', 'restore-only', 'read-only']) {
  test(`account actions remain reachable and permission scoped (${access})`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.addInitScript(() => localStorage.setItem('theme-mode', 'dark'))
    let authenticated = false
    const mutations = []
    const members = [
      { id: '12', principal_id: '2', principal_type: 'user', display_name: 'Active Researcher', username: 'active-user', status: 'active' },
      { id: '13', principal_id: '3', principal_type: 'user', display_name: 'Suspended Researcher', username: 'suspended-user', status: 'suspended' },
      { id: '14', principal_id: '4', principal_type: 'user', display_name: 'Ended Researcher', username: 'ended-user', status: 'ended' }
    ]
    const permissions = ['iam.tenant_membership.read', 'iam.tenant_role_assignment.read']
    if (access === 'full') permissions.push('iam.tenant_membership.update', 'iam.tenant_membership.suspend', 'iam.tenant_membership.restore', 'iam.tenant_membership.close', 'iam.tenant_role_assignment.create', 'iam.tenant_role_assignment.revoke')
    if (access === 'restore-only') permissions.push('iam.tenant_membership.restore')

    await page.route('**/api/v1/system/**', async (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      if (request.method() === 'OPTIONS') return fulfillJSON(route, 204, {})
      if (path.endsWith('/refresh')) return fulfillJSON(route, authenticated ? 200 : 401, authenticated ? { access_token: 'account-actions-token', expires_in: 300 } : { error: 'authentication_required' })
      if (path.endsWith('/login')) {
        authenticated = true
        return fulfillJSON(route, 200, { next_action: 'session_issued', session: { access_token: 'account-actions-token', expires_in: 300 } })
      }
      if (path.endsWith('/users/me')) return fulfillJSON(route, 200, { id: '1', username: 'e2e-admin', display_name: 'E2E Administrator' })
      if (path.endsWith('/auth/context-options')) return fulfillJSON(route, 200, { contexts: [{ type: 'tenant', tenant_id: '1', tenant_membership_id: '11', current: true }] })
      if (path.endsWith('/auth/context')) return fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' }, authorization: { role_assignments: [{ role_key: 'tenant.administrator', scope: { type: 'tenant', tenant_id: '1' }, permissions }] }
      })
      if (path.endsWith('/tenant/memberships') && request.method() === 'GET') return fulfillJSON(route, 200, { data: members, total: members.length, total_pages: 1 })
      if (path.endsWith('/tenant/role_assignments')) return fulfillJSON(route, 200, { data: [], total: 0, total_pages: 1 })
      const mutation = path.match(/\/tenant\/memberships\/(\d+)\/(suspend|restore|close)$/)
      if (mutation && request.method() === 'POST') {
        const [, id, action] = mutation
        expect(permissions).toContain(`iam.tenant_membership.${action}`)
        const input = request.postDataJSON()
        mutations.push({ id, action, ...input })
        const member = members.find((item) => item.id === id)
        member.status = { suspend: 'suspended', restore: 'active', close: 'ended' }[action]
        return fulfillJSON(route, 200, member)
      }
      throw new Error(`unexpected account action request: ${request.method()} ${path}`)
    })

    await page.goto('/login?redirect=%2Fiam%2Faccounts')
    await page.locator('input[autocomplete="username"]').fill('e2e-admin')
    await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
    await page.locator('button.auth-login-primary').click()
    await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible()
    const activeRow = page.getByRole('row').filter({ has: page.getByText('Active Researcher', { exact: true }) })
    const suspendedRow = page.getByRole('row').filter({ has: page.getByText('Suspended Researcher', { exact: true }) })
    const endedRow = page.getByRole('row').filter({ has: page.getByText('Ended Researcher', { exact: true }) })
    await expect(endedRow.getByRole('button', { name: '更多', exact: true })).toHaveCount(0)
    await expect(endedRow.getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
    await expect(endedRow.getByRole('button', { name: '查看角色', exact: true })).toBeVisible()

    if (access === 'read-only') {
      await expect(page.getByRole('button', { name: '更多', exact: true })).toHaveCount(0)
      await expect(page.getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
      await expect(activeRow.getByRole('button', { name: '查看角色', exact: true })).toBeVisible()
      expect(mutations).toEqual([])
      return
    }
    if (access === 'restore-only') {
      await expect(activeRow.getByRole('button', { name: '更多', exact: true })).toHaveCount(0)
      await suspendedRow.getByRole('button', { name: '更多', exact: true }).click()
      await expect(page.getByRole('menuitem', { name: '恢复', exact: true })).toBeVisible()
      await expect(page.getByRole('menuitem', { name: '暂停', exact: true })).toHaveCount(0)
      await expect(page.getByRole('menuitem', { name: '结束', exact: true })).toHaveCount(0)
      expect(mutations).toEqual([])
      return
    }

    await expect(activeRow.getByRole('button', { name: '角色分配', exact: true })).toBeVisible()
    await expect(activeRow.getByRole('button', { name: '编辑', exact: true })).toBeVisible()
    await expect(activeRow.getByRole('button', { name: '暂停', exact: true })).toHaveCount(0)
    const actions = activeRow.locator('.iam-account-actions')
    const bounds = await actions.boundingBox()
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(1280)
    const buttonBounds = await actions.getByRole('button').all()
    for (const button of buttonBounds) {
      const box = await button.boundingBox()
      expect(Math.abs(box.y + box.height / 2 - bounds.y - bounds.height / 2)).toBeLessThanOrEqual(2)
      expect(box.x + box.width).toBeLessThanOrEqual(1280)
    }
    await activeRow.getByRole('button', { name: '更多', exact: true }).press('Enter')
    await expect(page.getByRole('menuitem', { name: '暂停', exact: true })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: '结束', exact: true })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: '恢复', exact: true })).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('account-actions-menu.png') })
    await page.getByRole('menuitem', { name: '暂停', exact: true }).click()
    const pauseDialog = page.getByRole('dialog', { name: '暂停', exact: true })
    await pauseDialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(pauseDialog).toBeVisible()
    expect(mutations).toEqual([])
    await pauseDialog.getByRole('button', { name: '取消', exact: true }).click()
    expect(mutations).toEqual([])

    for (const action of [
      { name: '暂停', command: 'suspend', status: '已暂停' },
      { name: '恢复', command: 'restore', status: '有效' },
      { name: '结束', command: 'close', status: '已结束' }
    ]) {
      await activeRow.getByRole('button', { name: '更多', exact: true }).click()
      await page.getByRole('menuitem', { name: action.name, exact: true }).click()
      const dialog = page.getByRole('dialog', { name: action.name, exact: true })
      await dialog.getByRole('textbox').fill(`E2E ${action.command}`)
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(activeRow).toContainText(action.status)
    }
    await expect(activeRow.getByRole('button', { name: '更多', exact: true })).toHaveCount(0)
    expect(mutations).toEqual([
      { id: '12', action: 'suspend', reason: 'E2E suspend' },
      { id: '12', action: 'restore', reason: 'E2E restore' },
      { id: '12', action: 'close', reason: 'E2E close' }
    ])
    await page.evaluate(() => localStorage.setItem('addp-lang', 'en'))
    await page.reload()
    await expect(suspendedRow.getByRole('button', { name: 'Manage Roles', exact: true })).toBeVisible()
    await expect(suspendedRow.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()
    const englishActions = suspendedRow.locator('.iam-account-actions')
    const englishBounds = await englishActions.boundingBox()
    for (const button of await englishActions.getByRole('button').all()) {
      const box = await button.boundingBox()
      expect(Math.abs(box.y + box.height / 2 - englishBounds.y - englishBounds.height / 2)).toBeLessThanOrEqual(2)
      expect(box.x + box.width).toBeLessThanOrEqual(1280)
    }
    await suspendedRow.getByRole('button', { name: 'More', exact: true }).click()
    await expect(page.getByRole('menuitem', { name: 'Restore', exact: true })).toBeVisible()
  })
}
