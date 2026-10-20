import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const engine = (id, groups, origin = 'general', builtin = false, state = 'active') => ({
  id: String(id), name: `Engine ${id}`, engine_type: 'fixture_engine', engine_origin: origin,
  is_builtin: builtin, lifecycle_state: state, connection_status: 'online',
  capabilities: { schema_version: 'engine.capabilities/v1', ...Object.fromEntries(groups.map(group => [group, {}])) }
})

const snapshot = () => [
  ...Array.from({ length: 12 }, (_, i) => engine(i + 1, ['storage'])),
  engine(13, ['storage', 'compute']),
  engine(14, ['compute'], 'extension', true),
  engine(15, ['storage'], 'general', true, 'disabled'),
  engine(16, ['storage'], 'extension', false, 'deleting'),
  engine(17, ['storage'], 'general', false, 'deleted')
]

async function fixture(page, language = 'zh-cn') {
  let rows = snapshot(), pendingRead, releaseRead
  const requests = []
  await page.clock.install()
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    const headers = {
      'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS'
    }
    const reply = json => route.fulfill({ json, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'engine-filter-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({
      principal: { id: '1', principal_type: 'user' },
      context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: ['system.engine.read'] }] }
    })
    if (path.endsWith('/engine-types')) return reply([])
    if (path.endsWith('/engines')) {
      requests.push(Object.fromEntries(url.searchParams))
      if (pendingRead) await pendingRead
      return reply(rows)
    }
    throw new Error(`Unexpected engine filter request: ${request.method()} ${path}`)
  })
  await page.goto('/engines')
  await expect(page.locator('.el-pagination__total')).toContainText('16')
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  return {
    requests,
    replace: next => { rows = next },
    delayNextRead: () => { pendingRead = new Promise(resolve => { releaseRead = resolve }) },
    release: () => { releaseRead(); pendingRead = null }
  }
}

const group = (page, label) => page.getByRole('group', { name: label, exact: true })
const rows = page => page.locator('.el-table__body-wrapper .el-table__row')
const total = page => page.locator('.el-pagination__total')

test('combines four dimensions locally, treats empty selections as empty and paginates after filtering', async ({ page }) => {
  const state = await fixture(page)
  await expect(rows(page)).toHaveCount(10)
  await page.locator('.el-pager').getByText('2', { exact: true }).click()
  await expect(rows(page)).toHaveCount(6)

  await group(page, '能力').getByText('计算', { exact: true }).click()
  await expect(total(page)).toHaveText('共 15 条')
  await expect(rows(page).first()).toContainText('Engine 1')
  await expect(page.locator('.el-pager .is-active')).toHaveText('1')

  await group(page, '注册方式').getByText('内置', { exact: true }).click()
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('Engine 15')
  await group(page, '能力').getByText('存储', { exact: true }).click()
  await expect(total(page)).toHaveText('共 0 条')
  await group(page, '能力').getByText('计算', { exact: true }).click()
  await expect(rows(page).first()).toContainText('Engine 14')

  await group(page, '来源').getByText('扩展', { exact: true }).click()
  await expect(rows(page)).toHaveCount(0)
  await group(page, '来源').getByText('通用', { exact: true }).click()
  await expect(rows(page)).toHaveCount(0)
  await group(page, '来源').getByText('扩展', { exact: true }).click()
  await expect(rows(page).first()).toContainText('Engine 14')

  for (const label of ['已启用', '已停用', '清理中']) {
    await group(page, '生命周期').getByText(label, { exact: true }).click()
  }
  await expect(rows(page)).toHaveCount(0)
  await group(page, '生命周期').getByText('已启用', { exact: true }).click()
  await expect(rows(page).first()).toContainText('Engine 14')
  await group(page, '注册方式').getByText('注册', { exact: true }).click()
  await expect(rows(page)).toHaveCount(0)
  expect(state.requests).toEqual([{ lifecycle_states: 'active,disabled,deleting,deleted' }])
})

test('filters deleted engines explicitly and retains changes made while a full refresh is pending', async ({ page }) => {
  const state = await fixture(page)
  for (const label of ['已启用', '已停用', '清理中']) {
    await group(page, '生命周期').getByText(label, { exact: true }).click()
  }
  await group(page, '生命周期').getByText('已删除', { exact: true }).click()
  await expect(total(page)).toHaveText('共 1 条')
  await expect(rows(page).first()).toContainText('Engine 17')

  state.delayNextRead()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.requests.length).toBe(2)
  await group(page, '能力').getByText('存储', { exact: true }).click()
  await group(page, '来源').getByText('扩展', { exact: true }).click()
  await group(page, '注册方式').getByText('注册', { exact: true }).click()
  state.replace([...snapshot(), engine(18, ['compute'], 'general', false, 'deleted')])
  state.release()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toContainText('Engine 18')
  await expect(group(page, '生命周期').getByRole('checkbox', { name: '已删除', exact: true })).toBeChecked()
  expect(state.requests).toEqual(Array(2).fill({ lifecycle_states: 'active,disabled,deleting,deleted' }))
})

test('clamps the current page when a refresh shrinks the filtered result', async ({ page }) => {
  const state = await fixture(page)
  await page.locator('.el-pager').getByText('2', { exact: true }).click()
  state.replace(snapshot().slice(0, 4))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(total(page)).toHaveText('共 4 条')
  await expect(rows(page)).toHaveCount(4)
  await expect(page.locator('.el-pager .is-active')).toHaveText('1')
  await expect(rows(page).first()).toContainText('Engine 1')
})

test('English filter groups wrap within a narrow dark viewport', async ({ page }, testInfo) => {
  await fixture(page, 'en')
  await page.setViewportSize({ width: 640, height: 800 })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect(group(page, 'Capabilities')).toBeVisible()
  await expect(group(page, 'Origin')).toBeVisible()
  await expect(group(page, 'Registration')).toBeVisible()
  await expect(group(page, 'Lifecycle')).toBeVisible()
  const filters = page.getByTestId('engine-filters')
  expect(await filters.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('engine-filters-narrow-dark.png') })
})
