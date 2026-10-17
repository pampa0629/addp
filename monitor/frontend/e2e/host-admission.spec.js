import { expect, test } from '@playwright/test'
import { backend, id, identity, node, permissions, setIdentity } from './monitoring-targets.fixture'

const createdID = '10000000-0000-4000-8000-000000000007'
const proposal = () => ({ schema: 'addp.host-admission/v1', host: { display_name: 'Desktop VM proposal', node_kind: 'virtual', addresses: [], allowed_module_bindings: [] },
  collection: { monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://127.0.0.1:19091/metrics' } } })
const upload = (page, value = proposal()) => page.getByTestId('admission-file').setInputFiles({ name: 'host-admission.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(value)) })

async function admissionBackend(page, options = {}) {
  const hosts = options.hosts || [{ node_id: node, display_name: 'Existing VM', node_kind: 'virtual', addresses: ['node.test'], enabled: true, version: 1 }]
  const state = await backend(page, { ...options, empty: true, hosts, identity: options.identity || identity([...permissions, 'platform.host_node.create']) })
  const hostWrites = []
  let release, finished
  const held = options.holdHost ? new Promise(resolve => { release = resolve }) : null
  const done = new Promise(resolve => { finished = resolve })
  await page.route('**/api/v1/system/platform/host_nodes', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    const input = route.request().postDataJSON(); hostWrites.push(input)
    const saved = { ...input, node_id: createdID, version: 1 }
    hosts.push(saved)
    if (held) await held
    await route.fulfill({ status: options.hostStatus || 201, json: options.hostStatus ? { error: 'Host save outcome unknown' } : saved })
    finished()
  })
  return { ...state, hostWrites, release: () => release?.(), hostFinished: done }
}

test('a manifest fills collection inputs and uses an explicitly selected existing host', async ({ page }) => {
  const state = await admissionBackend(page, { identity: identity() })
  await page.goto(`/monitoring-targets/new?node_id=${node}`)
  await upload(page)
  await expect(page.getByTestId('target-admission')).toContainText('Desktop VM proposal')
  await expect(page.getByRole('radio', { name: '按清单新建主机' })).toHaveCount(0)
  await expect(page.getByTestId('target-endpoint')).toHaveValue(proposal().collection.source.endpoint)
  await page.getByTestId('target-save').click()
  await expect(page).toHaveURL(new RegExp(`/monitoring-targets/${id}$`))
  expect(state.hostWrites).toEqual([])
  expect(state.writes).toEqual([{ method: 'POST', input: { subject: { kind: 'node', node_id: node }, ...proposal().collection, enabled: true } }])
})

test('one confirmation creates a System identity then collects that exact host, blocking duplicate clicks', async ({ page }) => {
  const state = await admissionBackend(page, { holdHost: true })
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  const save = page.getByTestId('target-save')
  await save.click()
  await expect.poll(() => state.hostWrites.length).toBe(1)
  await expect(save).toHaveClass(/is-loading/)
  await expect(page.getByRole('button', { name: '导入部署清单', exact: true })).toBeDisabled()
  expect(state.writes).toEqual([])
  state.release()
  await expect(page).toHaveURL(new RegExp(`/monitoring-targets/${id}$`))
  expect(state.hostWrites).toEqual([{ ...proposal().host, enabled: true }])
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].input.subject.node_id).toBe(createdID)
})

test('a rejected collection leaves the created host and an explicit retry never creates it again', async ({ page }) => {
  const state = await admissionBackend(page)
  let reject = true
  await page.route('**/api/v1/monitor/platform/monitoring_targets', route => route.request().method() === 'POST' && reject
    ? route.fulfill({ status: 400, json: { error: 'Source admission rejected' } }) : route.fallback())
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  await page.getByTestId('target-save').click()
  await expect(page.getByTestId('admission-created')).toContainText(createdID)
  await expect(page.getByTestId('admission-progress')).toContainText('采集配置尚未完成')
  await expect(page.getByTestId('target-save-error')).toContainText('Source admission rejected')
  await expect(page.getByTestId('target-node').locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
  reject = false
  await page.getByTestId('target-save').click()
  await expect(page).toHaveURL(new RegExp(`/monitoring-targets/${id}$`))
  expect(state.hostWrites).toHaveLength(1)
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].input.subject.node_id).toBe(createdID)
})

for (const [status, errorCode, notice] of [[503, 'obs_unavailable', '保存结果待核实'], [409, 'resource_version_conflict', '配置发生冲突']]) test(`a collection response blocks another write and offers verification (${status})`, async ({ page }) => {
  const state = await admissionBackend(page)
  let collectionAttempts = 0
  await page.route('**/api/v1/monitor/platform/monitoring_targets', route => {
    if (route.request().method() !== 'POST') return route.fallback()
    collectionAttempts++
    return route.fulfill({ status, json: { error: 'Save not confirmed', error_code: errorCode } })
  })
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  await page.getByTestId('target-save').click()
  await expect(page.getByTestId('admission-progress')).toContainText(notice)
  await expect(page.getByTestId('target-save')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '导入部署清单', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '查看采集配置列表', exact: true }).click()
  await expect(page).toHaveURL(/\/monitoring-targets$/)
  expect(collectionAttempts).toBe(1)
  expect(state.hostWrites).toHaveLength(1)
})

test('an uncertain host response stops before collection and never retries registration', async ({ page }) => {
  const state = await admissionBackend(page, { hostStatus: 503 })
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  await page.getByTestId('target-save').click()
  await expect(page.getByTestId('admission-progress')).toContainText('主机登记结果待核实')
  await expect(page.getByTestId('target-save')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '导入部署清单', exact: true })).toBeDisabled()
  expect(state.hostWrites).toHaveLength(1)
  expect(state.writes).toEqual([])
})

for (const host of [{ node_kind: 'physical', enabled: true }, { node_kind: 'virtual', enabled: false }]) test(`existing host must match kind and management state (${JSON.stringify(host)})`, async ({ page }) => {
  const state = await admissionBackend(page, { hosts: [{ node_id: node, display_name: 'Mismatching host', addresses: [], version: 1, ...host }] })
  await page.goto(`/monitoring-targets/new?node_id=${node}`)
  await upload(page)
  await page.getByTestId('target-save').click()
  await expect(page.getByTestId('target-save-error')).toContainText('所选主机必须已启用')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('invalid or oversized input never creates a host or collection configuration', async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto(`/monitoring-targets/new?node_id=${node}`)
  await upload(page, { ...proposal(), private_key: 'never accepted' })
  await expect(page.getByTestId('target-admission')).toContainText('部署清单无效')
  await expect(page.getByTestId('target-save')).toHaveCount(0)
  await upload(page, 'x'.repeat(16384))
  await expect(page.getByTestId('target-admission')).toContainText('部署清单无效')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('an empty edited endpoint is rejected before registering a new host', async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  await page.getByTestId('target-endpoint').fill('')
  await page.getByTestId('target-save').click()
  await expect(page.getByTestId('target-save-error')).toBeVisible()
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('an identity change during host creation cannot create a collection or restore the old proposal', async ({ page }) => {
  const state = await admissionBackend(page, { holdHost: true })
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await page.getByText('按清单新建主机', { exact: true }).click()
  await page.getByTestId('target-save').click()
  await expect.poll(() => state.hostWrites.length).toBe(1)
  await setIdentity(page, identity(permissions, { type: 'tenant', tenant_id: '8' }))
  await expect(page.getByTestId('target-admission')).toHaveCount(0)
  state.release()
  await state.hostFinished
  await setIdentity(page, identity([...permissions, 'platform.host_node.create']))
  await expect(page.getByTestId('target-endpoint')).toHaveValue('')
  await expect(page.getByTestId('target-admission')).not.toContainText('Desktop VM proposal')
  expect(state.writes).toEqual([])
})

test('a late file read is discarded when the identity changes', async ({ page }) => {
  const state = await admissionBackend(page)
  await page.goto('/monitoring-targets/new')
  await page.evaluate(() => { File.prototype.text = () => new Promise(resolve => { window.finishAdmissionRead = resolve }) })
  await upload(page)
  await expect.poll(() => page.evaluate(() => typeof window.finishAdmissionRead)).toBe('function')
  await setIdentity(page, identity(permissions, { type: 'tenant', tenant_id: '8' }))
  await page.evaluate(text => window.finishAdmissionRead(text), JSON.stringify(proposal()))
  await setIdentity(page, identity())
  await expect(page.getByTestId('target-endpoint')).toHaveValue('')
  await expect(page.getByTestId('target-admission')).not.toContainText('Desktop VM proposal')
  expect(state.hostWrites).toEqual([]); expect(state.writes).toEqual([])
})

test('English admission facts fit a narrow dialog', async ({ page }) => {
  await admissionBackend(page, { locale: 'en' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/monitoring-targets/new')
  await upload(page)
  await expect(page.getByTestId('target-admission')).toContainText('Virtual host')
  await expect(page.getByTestId('target-admission')).not.toContainText('monitor.targets.')
  expect(await page.getByRole('dialog').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
})
