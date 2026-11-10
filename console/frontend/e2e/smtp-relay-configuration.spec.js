import { expect, test } from '@playwright/test'

const relayPath = '/api/v1/monitor/settings/smtp-relay'
const credentialPath = `${relayPath}/credential`
const settings = {
  version: 7,
  enabled: true,
  host: 'smtp.example.test',
  port: 465,
  tls_mode: 'tls',
  from_address: 'monitor@example.test',
  from_name: 'Monitor',
  username: 'monitor'
}

async function openRelay(page, { writable = false } = {}) {
  const context = { type: 'platform' }
  const permissions = ['monitor.configuration.read']
  if (writable) permissions.push('monitor.configuration.update')
  const writes = []
  let failRead = false
  let relay = { ...settings, credential: { configured: true, version: 3 }, pending_restart: true }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    switch (path) {
      case '/api/v1/system/refresh':
        return route.fulfill({ json: { access_token: 'smtp-relay-test-token', expires_in: 3600 } })
      case '/api/v1/system/users/me':
        return route.fulfill({ json: { id: '31', display_name: 'smtp-test-user' } })
      case '/api/v1/system/auth/context':
        return route.fulfill({ json: { context, authorization: { role_assignments: [{ scope: context, permissions }] } } })
      case '/api/v1/monitor/settings/runtime-policy':
        return route.fulfill({ json: { version: 1 } })
      case relayPath:
        if (request.method() === 'GET') {
          return failRead
            ? route.fulfill({ status: 503, json: { error: '无法读取 SMTP 配置' } })
            : route.fulfill({ json: relay })
        }
        break
    }
    if ([relayPath, credentialPath].includes(path) && request.method() !== 'GET') {
      const payload = request.postDataJSON()
      writes.push({ path, method: request.method(), payload })
      if (writable && request.method() === 'PUT') {
        if (path === credentialPath) {
          relay.credential = { configured: true, version: relay.credential.version + 1 }
          return route.fulfill({ json: relay.credential })
        }
        relay = { ...relay, ...payload, version: relay.version + 1 }
        return route.fulfill({ json: relay })
      }
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.goto('/configuration/monitor')
  await page.getByRole('tab', { name: 'SMTP 投递', exact: true }).click()
  return { writes, setReadFailure: value => { failRead = value } }
}

test('SMTP read-only session can view saved settings and credential status without issuing writes', async ({ page }) => {
  const { writes } = await openRelay(page)
  const panel = page.getByRole('tabpanel')
  const host = panel.getByRole('textbox', { name: '服务器地址' })
  await expect(host).toHaveValue(settings.host)
  await expect(host).toBeDisabled()
  await expect(panel.getByRole('spinbutton', { name: '端口' })).toBeDisabled()
  await expect(panel.getByRole('switch')).toBeDisabled()
  await expect(panel.getByText('TLS', { exact: true })).toBeVisible()
  await expect(panel.getByRole('combobox')).toHaveCount(0)
  await expect(panel.getByText('已配置', { exact: true })).toBeVisible()
  const credential = panel.getByPlaceholder('留空表示保持当前凭据')
  await expect(credential).toHaveValue('')
  await expect(credential).toBeDisabled()
  await expect(panel.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(writes).toEqual([])
})

test('SMTP refresh failure hides stale settings and prevents a platform editor from writing', async ({ page }) => {
  const { writes, setReadFailure } = await openRelay(page, { writable: true })
  const panel = page.getByRole('tabpanel')
  const host = panel.getByRole('textbox', { name: '服务器地址' })
  const save = panel.getByRole('button', { name: '保存', exact: true })
  await expect(host).toHaveValue(settings.host)
  await expect(save).toBeEnabled()
  setReadFailure(true)
  await panel.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('无法读取 SMTP 配置', { exact: true })).toBeVisible()
  await expect(host).toHaveCount(0)
  await expect(save).toHaveCount(0)
  expect(writes).toEqual([])
})

test('SMTP editor saves the loaded settings version and sends a new password separately', async ({ page }) => {
  const { writes } = await openRelay(page, { writable: true })
  const panel = page.getByRole('tabpanel')
  const host = panel.getByRole('textbox', { name: '服务器地址' })
  const credential = panel.getByPlaceholder('留空表示保持当前凭据')
  await expect(host).toHaveValue(settings.host)
  await host.fill('new-smtp.example.test')
  await credential.fill('test-only-password')
  await panel.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => writes).toEqual([
    { path: relayPath, method: 'PUT', payload: { ...settings, host: 'new-smtp.example.test' } },
    { path: credentialPath, method: 'PUT', payload: { credential: 'test-only-password' } }
  ])
  await expect(panel.getByText('v4', { exact: true })).toBeVisible()
  await expect(credential).toHaveValue('')
})
