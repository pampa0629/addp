import { expect, test } from '@playwright/test'
import { mockRaster } from './raster-policy.fixture'

const fields = {
  version: 7,
  access_token_ttl_minutes: 24,
  delegated_access_token_ttl_minutes: 2,
  resource_access_ticket_ttl_minutes: 12,
  refresh_token_ttl_days: 45,
  oauth_authorization_code_ttl_minutes: 5,
  oauth_device_code_ttl_minutes: 10,
  oauth_device_poll_interval_seconds: 5,
  tenant_invitation_ttl_hours: 168,
  oauth_public_rate_limit_per_minute: 60,
  oauth_user_rate_limit_per_minute: 30
}

async function mockPolicy(page, { writable = false } = {}) {
  await mockRaster(page, { permissions: ['iam.security_policy.read', ...(writable ? ['iam.security_policy.update'] : [])] })
  let failRead = false
  let policy = { ...fields, applied_version: 7, pending_restart: false }
  const writes = []
  await page.route('**/api/v1/system/platform/security_policy', route => {
    const request = route.request()
    if (request.method() === 'GET') {
      return failRead
        ? route.fulfill({ status: 503, json: { error: 'unavailable' } })
        : route.fulfill({ json: policy })
    }
    if (request.method() === 'PUT') {
      const payload = request.postDataJSON()
      writes.push(payload)
      if (writable) {
        policy = { ...policy, ...payload, version: policy.version + 1, pending_restart: true }
        return route.fulfill({ json: policy })
      }
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  return { writes, failNextRead: () => { failRead = true } }
}

test('IAM security reader sees loaded values and version with zero writes', async ({ page }) => {
  const { writes } = await mockPolicy(page)
  await page.goto('/iam/security')
  const numbers = page.getByRole('spinbutton')
  await expect(numbers).toHaveCount(10)
  await expect(numbers.first()).toHaveValue('24')
  for (const field of await numbers.all()) await expect(field).toBeDisabled()
  await expect(page.getByText('配置版本 7 / 已应用版本 7', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '保存策略' })).toHaveCount(0)
  expect(writes).toEqual([])
})

test('IAM security editor saves the loaded version and sees the restart status', async ({ page }) => {
  const { writes } = await mockPolicy(page, { writable: true })
  await page.goto('/configuration')
  const accessTTL = page.getByRole('spinbutton', { name: 'Access Token 有效期（分钟）', exact: true })
  await expect(accessTTL).toHaveValue('24')
  await accessTTL.fill('30')
  await page.getByRole('button', { name: '保存策略' }).click()
  await expect(page.getByText('配置版本 8 / 已应用版本 7', { exact: true })).toBeVisible()
  await expect(page.getByText('待重启生效', { exact: true })).toBeVisible()
  expect(writes).toEqual([{ ...fields, access_token_ttl_minutes: 30 }])
})

test('IAM security refresh failure hides stale policy and prevents editor writes', async ({ page }) => {
  const { writes, failNextRead } = await mockPolicy(page, { writable: true })
  await page.goto('/configuration')
  const save = page.getByRole('button', { name: '保存策略' })
  await expect(save).toBeEnabled()
  failNextRead()
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page.getByText('加载 IAM 安全策略失败', { exact: true })).toBeVisible()
  await expect(page.getByRole('spinbutton')).toHaveCount(0)
  await expect(page.getByText(/^配置版本 /)).toHaveCount(0)
  await expect(save).toHaveCount(0)
  expect(writes).toEqual([])
})
