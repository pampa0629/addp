import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const ID = '9007199254740993'
const row = page => page.locator('.el-table__body-wrapper .el-table__row').filter({ hasText: 'Math fixture' })
const dialog = page => page.getByTestId('engine-disable-dialog')
const confirm = page => page.getByTestId('engine-disable-confirm')
const refresh = page => page.getByTestId('engine-disable-refresh')

async function fixture(page, { permitted = true, builtin = true, language = 'zh-cn' } = {}) {
  const state = {
    engine: { id: ID, name: 'Math fixture', engine_type: 'math_workflow', engine_origin: 'extension', is_builtin: builtin, version: 7, lifecycle_state: 'active', connection_status: 'online', capabilities: { compute: {} } },
    writes: [], reads: [], conflict: false, uncertain: false, detailFails: false, denied: false,
    pendingList: null, releaseList: null
  }
  await page.clock.install()
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const headers = {
      'access-control-allow-origin': req.headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,PUT,OPTIONS'
    }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'engine-disable-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({
      principal: { id: '1', principal_type: 'user' },
      context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.engine.read', ...(permitted ? ['system.engine.update'] : [])] }] }
    })
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines')) {
      state.reads.push('list')
      const snapshot = { ...state.engine }
      if (state.pendingList) await state.pendingList
      return reply([snapshot])
    }
    if (path.endsWith(`/engines/${ID}`)) {
      if (req.method() === 'PUT') {
        state.writes.push(req.postDataJSON())
        if (state.conflict) return reply({ error_code: 'resource_version_conflict' }, 409)
        if (state.denied) return reply({ error_code: 'permission_denied' }, 403)
        state.engine = { ...state.engine, version: state.engine.version + 1, lifecycle_state: 'disabled' }
        if (state.uncertain) return reply({ error_code: 'unavailable' }, 503)
        return reply(state.engine)
      }
      state.reads.push('detail')
      return state.detailFails ? reply({ error_code: 'unavailable' }, 503) : reply(state.engine)
    }
    throw new Error(`Unexpected request: ${req.method()} ${path}`)
  })
  await page.goto('/engines')
  await expect(row(page)).toBeVisible()
  return state
}

for (const builtin of [true, false]) {
  test(`independently disables ${builtin ? 'built-in' : 'registered'} extension and reads back list plus detail`, async ({ page }) => {
    const state = await fixture(page, { builtin })
    await expect(row(page).getByRole('button', { name: '编辑', exact: true })).toHaveCount(0)
    await row(page).getByTestId('engine-disable-open').click()
    await expect(dialog(page)).toContainText('Math fixture')
    await expect(dialog(page)).toContainText('不会停止引擎进程')
    await dialog(page).getByRole('button', { name: '取消', exact: true }).click()
    expect(state.writes).toEqual([])
    await row(page).getByTestId('engine-disable-open').click()
    await confirm(page).click()
    await expect(dialog(page)).toBeHidden()
    await expect(page.getByText('引擎已停用，列表与详情已确认', { exact: true })).toBeVisible()
    expect(state.writes).toEqual([{ version: 7, lifecycle_state: 'disabled' }])
    expect(state.reads).toEqual(['list', 'list', 'detail'])
    await expect(row(page)).toContainText('已停用')
    await expect(row(page).getByTestId('engine-disable-open')).toBeDisabled()
  })
}

test('read-only viewers cannot submit a lifecycle write', async ({ page }) => {
  const state = await fixture(page, { permitted: false })
  await expect(row(page).getByTestId('engine-disable-open')).toBeDisabled()
  expect(state.writes).toEqual([])
})

test('freezes the observed version across polling, retains conflicts and only uses a new version after explicit review', async ({ page }) => {
  const state = await fixture(page)
  await row(page).getByTestId('engine-disable-open').click()
  state.engine.version = 9
  await page.clock.fastForward(11000)
  await expect.poll(() => state.reads.length).toBeGreaterThan(1)
  state.conflict = true
  await confirm(page).click()
  await expect(dialog(page)).toContainText('引擎已被修改')
  await expect(confirm(page)).toBeDisabled()
  expect(state.writes).toEqual([{ version: 7, lifecycle_state: 'disabled' }])
  await refresh(page).click()
  await expect(confirm(page)).toBeEnabled()
  expect(state.writes).toHaveLength(1)
  state.conflict = false
  await confirm(page).click()
  await expect(dialog(page)).toBeHidden()
  expect(state.writes[1]).toEqual({ version: 9, lifecycle_state: 'disabled' })
})

test('an uncertain write is resolved by readback without repeating PUT', async ({ page }) => {
  const state = await fixture(page)
  state.uncertain = true
  await row(page).getByTestId('engine-disable-open').click()
  await confirm(page).click()
  await expect(dialog(page)).toContainText('停用请求未确认')
  await expect(confirm(page)).toBeDisabled()
  await refresh(page).click()
  await expect(dialog(page)).toBeHidden()
  expect(state.writes).toHaveLength(1)
  expect(state.reads).toEqual(['list', 'list', 'detail'])
})

test('successful PUT with failed detail readback keeps confirmation open and retries reads only', async ({ page }) => {
  const state = await fixture(page)
  state.detailFails = true
  await row(page).getByTestId('engine-disable-open').click()
  await confirm(page).click()
  await expect(dialog(page)).toContainText('无法确认最新状态')
  await expect(page.getByText('引擎已停用，列表与详情已确认', { exact: true })).toHaveCount(0)
  await expect(confirm(page)).toBeDisabled()
  await refresh(page).click()
  await expect(dialog(page)).toContainText('无法确认最新状态')
  state.detailFails = false
  state.engine.version = 7
  await refresh(page).click()
  await expect(dialog(page)).toContainText('无法确认最新状态')
  state.engine.version = 8
  await refresh(page).click()
  await expect(dialog(page)).toBeHidden()
  expect(state.writes).toHaveLength(1)
})

test('drains an older poll before readback so its active snapshot cannot overwrite disabled state', async ({ page }) => {
  const state = await fixture(page)
  state.pendingList = new Promise(resolve => { state.releaseList = resolve })
  await page.clock.fastForward(11000)
  await expect.poll(() => state.reads.length).toBe(2)
  await row(page).getByTestId('engine-disable-open').click()
  await confirm(page).click()
  await expect.poll(() => state.writes.length).toBe(1)
  await expect(dialog(page)).toBeVisible()
  state.pendingList = null
  state.releaseList()
  await expect(dialog(page)).toBeHidden()
  await expect(row(page)).toContainText('已停用')
  expect(state.reads).toEqual(['list', 'list', 'list', 'detail'])
})

test('server permission rejection is retained without automatically retrying the write', async ({ page }) => {
  const state = await fixture(page)
  state.denied = true
  await row(page).getByTestId('engine-disable-open').click()
  await confirm(page).click()
  await expect(dialog(page)).toContainText('停用请求未确认')
  await expect(confirm(page)).toBeDisabled()
  expect(state.writes).toHaveLength(1)
})

test('English confirmation fits a narrow dark viewport', async ({ page }, testInfo) => {
  await fixture(page, { language: 'en' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await row(page).getByTestId('engine-disable-open').click()
  await expect(dialog(page)).toContainText('Disable “Math fixture”?')
  expect(await dialog(page).evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('engine-disable-narrow-dark.png'), animations: 'disabled' })
})
