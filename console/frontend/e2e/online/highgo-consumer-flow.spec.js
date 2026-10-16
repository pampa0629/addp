import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('HighGo business tables retain relational facts and replaced orders render in Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_HIGHGO_EXPECTATIONS'))
  const gateway = required('GATEWAY_URL')
  const api = await request.newContext({ baseURL: gateway, extraHTTPHeaders: {
    Authorization: `Bearer ${required('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')}`
  } })
  const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'consumer identity'))
  await api.dispose()
  expect(current.principalType).toBe('user')
  expect(current.tenantID).toBe(String(expected.tenant_id))
  expect(current.principalID).toBe(expected.principal_id)
  const first = expected.samples.customers.locator
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'),
    required('ADDP_ONLINE_TEST_USER_PASSWORD'), `/manager/data-explorer?locator=${encodeURIComponent(first)}`)
  const browserAPI = await request.newContext({ baseURL: gateway, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(browserIdentity.principalID).toBe(current.principalID)
    expect(browserIdentity.tenantID).toBe(current.tenantID)
    expect([...browserIdentity.permissions].sort()).toEqual([...current.permissions].sort())
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.getByRole('treeitem', { name: 'customers', exact: true })).toBeVisible()
    const samples = { ...expected.samples, orders_replace_online: { locator: expected.target_locator, rows: 3, fields: 7 } }
    for (const [name, sample] of Object.entries(samples)) {
      // Read through the actual selected tree node, preserving scanned DataItem identity.
      await manager.getByRole('treeitem', { name, exact: true }).click()
      await manager.getByRole('tab', { name: /^(预览|Preview)$/ }).click()
      await expect(manager.getByText(`addp_business/${name} - 数据预览`, { exact: true })).toBeVisible()
      await expect(manager.getByText(`显示 ${sample.fields} / ${sample.fields} 个字段`, { exact: true })).toBeVisible()
      await expect(manager.getByText(`共 ${sample.rows} 行`, { exact: true })).toBeVisible()
      await expect(manager.getByText('数据预览失败', { exact: true })).toHaveCount(0)
    }
    await expect(manager.getByRole('cell', { name: 'SENTINEL', exact: true })).toHaveCount(0)
    await expect(manager.getByRole('cell', { name: 'HG-2026-0001', exact: true })).toHaveCount(1)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'highgo-replace-console.png') })
    await manager.getByRole('treeitem', { name: 'orders', exact: true }).click()
    await manager.getByRole('tab', { name: /^(属性|Attributes)$/ }).click()
    await manager.getByRole('button', { name: /^(查看 JSON|View JSON)$/ }).click()
    const dialog = manager.getByRole('dialog', { name: '完整属性 JSON', exact: true })
    await expect(dialog).toBeVisible()
    const attrs = JSON.parse(await dialog.locator('pre.attribute-json').innerText())
    expect(attrs.capabilities.indexing.indexes.find(row => row.name === 'idx_highgo_orders_updated_at').fields).toEqual(['updated_at', 'id'])
    expect(attrs.capabilities.constraints.constraints.find(row => row.constraint_type === 'foreign_key')).toEqual({
      name: 'orders_customer_id_fkey', constraint_type: 'foreign_key', fields: ['customer_id'],
      referenced_namespace: 'addp_business', referenced_table: 'customers', referenced_fields: ['id']
    })
    expect(attrs.capabilities.partitioning).toBeUndefined()
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'highgo-constraints-console.png') })
    writeFileSync(required('ADDP_ONLINE_HIGHGO_BROWSER_REPORT'), JSON.stringify({
      run_id: expected.run_id, tenant_id: expected.tenant_id, principal_id: current.principalID,
      engine_id: expected.engine_id, tables: Object.keys(samples)
    }))
  } finally {
    await browserAPI.dispose()
  }
})
