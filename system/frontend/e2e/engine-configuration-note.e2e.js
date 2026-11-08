import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const descriptor = { type: 'fixture_engine', display_name: 'Fixture engine', origin: 'general', registration_group: 'tabular',
  connection_spec: { schema_version: 'engine.connection/v1', fields: [
    { key: 'host', input: 'text', sensitive: false }, { key: 'port', input: 'number', default: 5432 },
    { key: 'password', input: 'password', sensitive: true }, { key: 'token', input: 'password', sensitive: true }
  ] }, capabilities: { schema_version: 'engine.capabilities/v1', storage: {} } }

async function fixture(page, { language = 'zh-cn', described = true, origin = 'general' } = {}) {
  const state = { reads: 0, denied: false, requests: [] }
  const engine = { id: '71', name: 'Configuration fixture', engine_type: 'fixture_engine', engine_origin: origin,
    lifecycle_state: 'active', connection_status: 'online', version: 1, is_builtin: false,
    capabilities: { schema_version: 'engine.capabilities/v1', storage: {} },
    capabilities_view: { summary: [{ id: 'storage', label_key: 'system.engine.capabilities.storage' }],
      sections: [{ id: 'query', title_key: 'system.engine.capabilities.query', status: 'available',
        items: [{ id: 'table', label_key: 'system.engine.capabilityView.values.table', status: 'available' }] }] },
    connection_info: { host: 'saved-host', password: 'secret-password-sentinel', token: 'secret-token-sentinel',
      removed_field: 'removed-field-sentinel' } }
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    state.requests.push(`${request.method()} ${path}`)
    if (path.endsWith('/refresh')) return reply({ access_token: 'configuration-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Configuration reader' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' },
      context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.engine.read'] }] } })
    if (path.endsWith('/engine-types')) return reply(described ? [descriptor] : [])
    if (path.endsWith('/engines')) return reply([engine])
    if (path.endsWith('/engines/71')) {
      state.reads += 1
      if (state.denied) return reply({ error_code: 'forbidden', message: 'Forbidden' }, 403)
      return reply({ ...engine, version: state.reads, connection_info: { ...engine.connection_info, host: `saved-host-${state.reads}` } })
    }
    throw new Error(`Unexpected configuration fixture request: ${request.method()} ${path}`)
  })
  await page.goto('/engines')
  await page.locator('.el-table__row').filter({ hasText: engine.name })
    .getByRole('button', { name: language === 'en' ? 'Detail' : '详情', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  return state
}

for (const language of ['zh-cn', 'en']) {
  test(`saves a fresh, secret-free configuration note in ${language} through the real download button`, async ({ page }) => {
    const state = await fixture(page, { language })
    const button = page.getByTestId('engine-configuration-note')
    await expect(button).toBeEnabled()
    const saved = page.waitForEvent('download')
    await button.click()
    const download = await saved
    expect(await download.failure()).toBeNull()
    expect(download.suggestedFilename()).toBe('engine-71-configuration.md')
    const content = await readFile(await download.path(), 'utf8')
    expect(content).toContain(language === 'en' ? '# Engine configuration note' : '# 引擎配置说明')
    expect(content).toContain('| host | saved\\-host\\-2 |')
    expect(content).toContain(language === 'en' ? '| Configuration version | 2 |' : '| 配置版本 | 2 |')
    for (const forbidden of ['secret-password-sentinel', 'secret-token-sentinel', 'removed-field-sentinel', '| port | 5432 |', 'system.engine.']) {
      expect(content).not.toContain(forbidden)
    }
    expect(state.reads).toBe(2)
    expect(state.requests.filter(request => !request.startsWith('GET ') && !request.endsWith('/refresh'))).toEqual([])
    expect(state.requests.some(request => /\/test|\/catalog|\/access_grants/.test(request))).toBe(false)
    await page.setViewportSize({ width: 1280, height: 800 })
    const dialog = page.getByRole('dialog')
    await expect(button).toBeVisible()
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  })
}

test('a denied detail reread creates no download and never exports the stale display', async ({ page }) => {
  const state = await fixture(page)
  const button = page.getByTestId('engine-configuration-note')
  await expect(button).toBeEnabled()
  state.denied = true
  const downloads = []
  page.on('download', download => downloads.push(download))
  await button.click()
  await expect(page.getByText('导出配置说明失败，请确认仍有引擎详情读取权限后重试。', { exact: true })).toBeVisible()
  expect(state.reads).toBe(2)
  expect(downloads).toEqual([])
})

test('does not invent field definitions or provide the general-engine export for extension runtimes', async ({ page }) => {
  await fixture(page, { described: false })
  await expect(page.getByTestId('engine-configuration-note')).toBeDisabled()
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.unrouteAll({ behavior: 'wait' })
  await fixture(page, { origin: 'extension' })
  await expect(page.getByTestId('engine-configuration-note')).toHaveCount(0)
})
