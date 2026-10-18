import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('GaussDB SELECT-only source retains precise values and field facts in Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_GAUSSDB_EXPECTATIONS'))
  const api = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: {
    Authorization: `Bearer ${required('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')}`
  } })
  const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'consumer identity'))
  await api.dispose()
  expect(current.principalType).toBe('user')
  expect(current.tenantID).toBe(String(expected.tenant_id))
  expect(current.principalID).toBe(expected.principal_id)
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'),
    `/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
  const browserAPI = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(browserIdentity.principalID).toBe(current.principalID)
    expect(browserIdentity.tenantID).toBe(current.tenantID)
    expect([...browserIdentity.permissions].sort()).toEqual([...current.permissions].sort())
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.getByRole('treeitem', { name: 'source', exact: true })).toBeVisible()
    await manager.getByRole('treeitem', { name: 'source', exact: true }).click()
    await manager.getByRole('tab', { name: /^(预览|Preview)$/ }).click()
    await expect(manager.getByText('addp_acceptance/source - 数据预览', { exact: true })).toBeVisible()
    await expect(manager.getByText('共 2 行', { exact: true })).toBeVisible()
    for (const value of ['9007199254740993', '12345678901234567890.123456789012345678', '高斯正式接入中文']) {
      await expect(manager.getByRole('cell', { name: value, exact: true })).toBeVisible()
    }
    await expect(manager.getByText('数据预览失败', { exact: true })).toHaveCount(0)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'gaussdb-preview-console.png') })
    await manager.getByRole('tab', { name: /^(属性|Attributes)$/ }).click()
    await manager.getByRole('button', { name: /^(查看 JSON|View JSON)$/ }).click()
    const dialog = manager.getByRole('dialog', { name: '完整属性 JSON', exact: true })
    await expect(dialog).toBeVisible()
    const attrs = JSON.parse(await dialog.locator('pre.attribute-json').innerText())
    expect(attrs.table.primary_key).toEqual(['name', 'id'])
    expect(attrs.table.fields.find(f => f.name === 'amount')).toMatchObject({ type: 'decimal', precision: 38, scale: 18 })
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'gaussdb-facts-console.png') })
    writeFileSync(required('ADDP_ONLINE_GAUSSDB_BROWSER_REPORT'), JSON.stringify(Object.fromEntries(
      ['run_id', 'tenant_id', 'principal_id', 'engine_id'].map(key => [key, expected[key]]))))
  } finally { await browserAPI.dispose() }
})
