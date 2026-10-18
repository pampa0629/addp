import { expect, test } from '@playwright/test'
import { identity, permissions } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'
import { admissionBackend, createdID, frameOf, proposal, upload } from './host-admission.fixture'

async function create(page) {
  await page.goto('/system/host-admission')
  await upload(page)
  const frame = frameOf(page)
  await frame.getByText('按清单新建主机', { exact: true }).click()
  await frame.getByTestId('admission-confirm').click()
  return frame
}

test('a host-only native declaration registers services without any Monitor permission or write', async ({ page }, testInfo) => {
  const state = await admissionBackend(page, { identity: identity(['platform.host_node.read', 'platform.host_node.create']) })
  await page.goto('/system/host-admission')
  const value = proposal(); delete value.collection
  value.host.node_kind = 'physical'; value.host.display_name = 'Native Mac'; value.host.allowed_module_bindings = [{ module_name: 'monitor', client_id: 'addp-monitor' }]
  await upload(page, value)
  const frame = frameOf(page)
  await expect(frame.getByTestId('host-admission')).toContainText('不配置主机采集')
  await frame.getByText('按清单新建主机', { exact: true }).click()
  await frame.getByTestId('admission-confirm').click()
  await expect(frame.getByTestId('admission-deployment')).toHaveValue(`ADDP_HOST_NODE_ID=${createdID}`)
  await expect(frame.getByTestId('admission-collect')).toHaveCount(0)
  expect(state.hostWrites).toEqual([{ ...value.host, enabled: true }]); expect(state.writes).toEqual([])
  await page.reload()
  await expect(frame.getByTestId('admission-deployment')).toHaveValue(`ADDP_HOST_NODE_ID=${createdID}`)
  expect(state.hostWrites).toHaveLength(1)
  await page.screenshot({ path: testInfo.outputPath('native-host-admission.png'), fullPage: true })
})

test('one host is frozen before collection; an explicit rejection retries only the exact Monitor subject', async ({ page }, testInfo) => {
  const state = await admissionBackend(page)
  let rejecting = true, attempts = 0
  await page.route('**/api/v1/monitor/platform/monitoring_targets', route => {
    if (route.request().method() !== 'POST') return route.fallback()
    attempts++
    return rejecting ? route.fulfill({ status: 400, json: { error: 'Source admission rejected', error_code: 'invalid_monitoring_target' } }) : route.fallback()
  })
  const frame = await create(page)
  await expect(frame.getByTestId('admission-created')).toContainText(createdID)
  expect(state.writes).toEqual([])
  await frame.getByTestId('admission-collect').click()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(proposal().collection.source.endpoint)
  expect(page.url()).not.toContain('19091')
  await expect(frame.getByTestId('target-node').locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
  await frame.getByTestId('target-save').click()
  await expect(frame.getByTestId('target-save-error')).toContainText('Source admission rejected')
  rejecting = false
  await frame.getByTestId('target-save').click()
  await expect(page).toHaveURL(/\/monitor\/monitoring-targets\/[0-9a-f-]+$/)
  expect(attempts).toBe(2); expect(state.hostWrites).toHaveLength(1)
  expect(state.writes[0].input.subject.node_id).toBe(createdID)
  await page.reload()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(proposal().collection.source.endpoint)
  await page.screenshot({ path: testInfo.outputPath('collection-after-host.png'), fullPage: true })
})

test('collection drafts are consumed once and never survive a browser reload or enter stored recent URLs', async ({ page }) => {
  await admissionBackend(page)
  const frame = await create(page)
  await frame.getByTestId('admission-collect').click()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(proposal().collection.source.endpoint)
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
  expect(storage).not.toContain('19091')
  await page.reload()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue('')
  await expect(frame.getByTestId('target-node')).toContainText('Desktop VM proposal')
})

test('an unknown host creation cannot be reimported or retried and never writes collection', async ({ page }) => {
  const state = await admissionBackend(page, { hostStatus: 503 })
  const frame = await create(page)
  await expect(frame.getByTestId('host-admission')).toContainText('主机登记结果待核实')
  await expect(frame.getByTestId('admission-confirm')).toHaveCount(0)
  await expect(frame.getByRole('button', { name: '导入部署清单', exact: true })).toBeDisabled()
  expect(state.hostWrites).toHaveLength(1); expect(state.writes).toEqual([])
})

test('pending registration blocks duplicate clicks and a late identity cannot restore the proposal', async ({ page }) => {
  const state = await admissionBackend(page, { holdHost: true })
  const frame = await create(page)
  await expect.poll(() => state.hostWrites.length).toBe(1)
  await expect(frame.getByTestId('admission-confirm')).toHaveClass(/is-loading/)
  await frame.locator('body').evaluate(() => { document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('system-auth').authContext = null })
  state.release()
  await expect(frame.getByTestId('host-admission')).toHaveCount(0)
  expect(state.writes).toEqual([])
})

for (const [index, value] of [{ schema: 'addp.host-admission/v1' }, { ...proposal(), password: 'forbidden' }, { ...proposal(), collection: {} }].entries()) test(`invalid manifest never creates an identity (${index})`, async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto('/system/host-admission'); await upload(page, value)
  await expect(frameOf(page).getByTestId('admission-error')).toContainText('部署清单无效')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('English manifest preview remains readable on a narrow screen', async ({ page }, testInfo) => {
  await admissionBackend(page, { locale: 'en' })
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/system/host-admission'); await upload(page)
  await expect(frameOf(page).getByTestId('host-admission')).toContainText('Virtual host')
  expect(await frameOf(page).locator('body').evaluate(el => el.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('host-admission-english-narrow.png'), fullPage: true })
})

for (const change of [{ node_kind: 'physical' }, { enabled: false }]) test(`existing host must satisfy its current state (${JSON.stringify(change)})`, async ({ page }) => {
  const host = { node_id: createdID, display_name: 'Existing host', node_kind: 'virtual', enabled: true, version: 1, addresses: [], allowed_module_bindings: [], ...change }
  const state = await admissionBackend(page, { hosts: [host] })
  await page.goto('/system/host-admission'); await upload(page)
  const frame = frameOf(page)
  await frame.getByTestId('target-node').click(); await frame.getByRole('option', { name: 'Existing host', exact: true }).click()
  await frame.getByTestId('admission-confirm').click()
  await expect(frame.getByTestId('admission-error')).toContainText('所选主机必须已启用')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('a late file result is discarded after authority disappears', async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto('/system/host-admission')
  const frame = frameOf(page)
  await frame.locator('body').evaluate(() => { File.prototype.text = () => new Promise(resolve => { window.finishHostFile = resolve }) })
  await upload(page)
  await expect.poll(() => frame.locator('body').evaluate(() => typeof window.finishHostFile)).toBe('function')
  await frame.locator('body').evaluate(() => { document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('system-auth').authContext = null })
  await frame.locator('body').evaluate(value => window.finishHostFile(value), JSON.stringify(proposal()))
  await expect(frame.getByTestId('host-admission')).toHaveCount(0)
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('an unknown collection outcome keeps the host and requires list verification before another save', async ({ page }) => {
  const state = await admissionBackend(page)
  let attempts = 0
  await page.route('**/api/v1/monitor/platform/monitoring_targets', route => {
    if (route.request().method() !== 'POST') return route.fallback()
    attempts++
    return route.fulfill({ status: 503, json: { error: 'Save outcome unknown' } })
  })
  const frame = await create(page)
  await frame.getByTestId('admission-collect').click()
  await expect(frame.getByTestId('target-endpoint')).toHaveValue(proposal().collection.source.endpoint)
  await frame.getByTestId('target-save').click()
  await expect(frame.getByTestId('target-create-unknown')).toContainText('采集配置保存结果待核实')
  await expect(frame.getByTestId('target-save')).toHaveCount(0)
  expect(attempts).toBe(1); expect(state.hostWrites).toHaveLength(1)
})

test('an existing host cannot gain declared module bindings implicitly', async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto('/system/host-admission')
  const value = proposal(); value.host.allowed_module_bindings = [{ module_name: 'monitor', client_id: 'addp-monitor' }]
  await upload(page, value)
  const frame = frameOf(page)
  await frame.getByTestId('target-node').click(); await frame.getByRole('option', { name: 'Existing VM', exact: true }).click()
  await frame.getByTestId('admission-confirm').click()
  await expect(frame.getByTestId('admission-error')).toContainText('允许清单中声明的服务模块关联')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})
