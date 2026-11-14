import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test('metadata-only native table stays visible but Manager refuses content', async ({ page }) => {
  const expected = JSON.parse(process.env.ADDP_ONLINE_NATIVE_PREVIEW_EXPECTATIONS)
  expect(process.env.ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN).toBeUndefined()
  expect(process.env.ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN).toBeUndefined()
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const response = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === expected.locator
  })
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
    process.env.ADDP_ONLINE_TEST_USER_PASSWORD, `/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'native preview consumer'))
    expect(current.principalType).toBe('user')
    expect(current.principalID).toBe(expected.principal_id)
    expect(current.tenantID).toBe(expected.tenant_id)
    expect([...current.permissions].sort()).toEqual(expected.permissions)
    const refused = await response
    expect(refused.status()).toBe(403)
    const payload = await refused.json()
    expect(payload.preview_type).toBeUndefined()
    expect(payload.data).toBeUndefined()
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.getByTestId('preview-failure')).toBeVisible()
    await expect(manager.locator('.preview-content')).toHaveCount(0)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR,
      `native-preview-${expected.engine_id}-metadata-only.png`) })
    writeFileSync(process.env.ADDP_ONLINE_NATIVE_PREVIEW_REPORT,
      JSON.stringify({ ...expected, preview_status: 403, rows_visible: false }))
  } finally {
    await api.dispose()
  }
})
