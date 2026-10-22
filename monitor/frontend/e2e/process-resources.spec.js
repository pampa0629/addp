import { expect, test } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'

import { backend, permissions as processPermissions } from './process-resources.fixture'

const boundHostID = '10000000-0000-4000-8000-000000000001'
const boundInstances = () => [{ id: 1, node_id: boundHostID, host_node_name: 'old-deployment-alias' }, { id: 2, node_id: boundHostID, host_node_name: 'old-deployment-alias' }, { id: 3, node_id: '', host_node_name: 'Current managed host' }]
const hostPermissions = [...processPermissions, 'platform.host_node.read']

for (const locale of ['zh-cn', 'en']) test(`service endpoints show registered addresses and never infer a Worker listener (${locale})`, async ({ page }, testInfo) => {
  await backend(page, { locale, instances: [
    { id: 1, role: 'backend', module_url: 'http://service.internal:8180/api?private=value#fragment' },
    { id: 2, role: 'backend', module_url: 'http://[2001:db8::7]:8190/api' },
    { id: 3, role: 'backend', module_url: 'https://service.example.test/api' },
    { id: 4, health_check_url: 'http://health.internal:8090/ready' },
    { id: 5, registered_host: '192.0.2.4', runtime_hostname: 'worker-container', host_node_name: 'deployment-alias' }
  ] })
  await page.goto('/service-resources?refresh=off')
  await expect(page.getByTestId('process-endpoint')).toHaveText(['service.internal:8180', '[2001:db8::7]:8190', 'service.example.test:443', 'health.internal:8090', locale === 'en' ? 'No registered listener' : '未登记监听端点'])
  await expect(page.getByTestId('process-list')).not.toContainText('private=value')
  await expect(page.getByTestId('process-endpoint').nth(4)).not.toContainText('192.0.2.4')
  await page.screenshot({ path: testInfo.outputPath(`service-endpoints-${locale}.png`), fullPage: true })
})

for (const [locale, labels, dark] of [['zh-cn', ['UP · 在线', 'UP · 在线', 'DOWN · 离线'], false], ['en', ['UP · Online', 'UP · Online', 'DOWN · Offline'], true]]) {
  test(`lease status badges remain distinct from collection failure (${locale})`, async ({ page }, testInfo) => {
    const state = await backend(page, { locale, instances: [{ id: 1 }, { id: 2 }, { id: 3, status: 'down' }] })
    state.setMode('failed')
    await page.addInitScript(dark => localStorage.setItem('theme-mode', dark ? 'dark' : 'light'), dark)
    await page.goto('/service-resources?status=all&refresh=off')
    await expect(page.getByTestId('process-list')).toContainText('1.50')
    await expect(page.getByTestId('process-online')).toHaveText(labels)
    await expect(page.getByTestId('process-online').nth(0)).toHaveClass(/el-tag--success/)
    await expect(page.getByTestId('process-online').nth(1)).toHaveClass(/el-tag--success/)
    await expect(page.getByTestId('process-online').nth(2)).toHaveClass(/el-tag--danger/)
    await expect(page.getByTestId('process-list')).toContainText(locale === 'en' ? 'Collection failed' : '采集失败')
    for (const tag of await page.getByTestId('process-online').all()) {
      expect(await tag.evaluate(el => {
        const cell = el.closest('.cell')
        return el.getBoundingClientRect().right <= cell.getBoundingClientRect().right - parseFloat(getComputedStyle(cell).paddingRight) + 1
      })).toBe(true)
    }
    if (dark) await expect(page.locator('html')).toHaveClass(/dark/)
    else await expect(page.locator('html')).not.toHaveClass(/dark/)
    await page.screenshot({ path: testInfo.outputPath(`service-status-${locale}.png`), fullPage: true })
  })
}

test('bound services resolve one current managed name and discard names when its read fails', async ({ page }) => {
  const hosts = [{ node_id: boundHostID, display_name: 'Current managed host', enabled: true, version: 1 }]
  const state = await backend(page, { instances: boundInstances(), hosts, identity: identity(hostPermissions) })
  await page.goto('/service-resources?refresh=off')
  const list = page.getByTestId('process-list')
  await expect(list.getByRole('button', { name: 'Current managed host', exact: true })).toHaveCount(2)
  await expect(list).not.toContainText('old-deployment-alias')
  await expect(page.getByTestId('process-host').nth(2)).toHaveText('未关联主机')
  expect(state.hostReads).toEqual([boundHostID])
  hosts[0].display_name = 'Renamed managed host'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list.getByRole('button', { name: 'Renamed managed host', exact: true })).toHaveCount(2)
  await expect(list).not.toContainText('Current managed host')
  expect(state.hostReads).toEqual([boundHostID, boundHostID])
  await page.route(`**/api/v1/system/platform/host_nodes/${boundHostID}`, route => route.fulfill({ status: 403, json: { error_code: 'permission_denied' } }))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list).toContainText('1.50 核')
  await expect(list).not.toContainText('Renamed managed host')
  await expect(page.getByTestId('process-host').first()).toHaveText(boundHostID)
  await expect(page.getByTestId('process-host').getByRole('button')).toHaveCount(0)
})

test('host names are never read without host permission and a late name cannot survive its removal', async ({ page }) => {
  const state = await backend(page, { instances: boundInstances(), hosts: [{ node_id: boundHostID, display_name: 'Late host name' }], holdHosts: true, identity: identity(hostPermissions) })
  await page.goto('/service-resources?refresh=off')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  await expect.poll(() => state.hostReads.length).toBe(1)
  await setIdentity(page, identity(processPermissions))
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  state.releaseHosts()
  await expect(page.getByTestId('process-host').getByRole('button')).toHaveCount(0)
  await expect(page.getByTestId('process-list')).not.toContainText('Late host name')
  expect(state.hostReads).toEqual([boundHostID])
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.calls.length).toBe(3)
  expect(state.hostReads).toEqual([boundHostID])
})

async function visibility(page, hidden) {
  await page.evaluate(hidden => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: hidden })
    document.dispatchEvent(new Event('visibilitychange'))
  }, hidden)
}

test('automatic refresh preserves unsubmitted filters and only Search applies them', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?role=worker')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  const module = page.locator('.process-filters .el-input input').first()
  const status = page.locator('.process-filters .el-select:has(input[aria-label="在线状态"])')
  await module.fill('meta')
  await status.click()
  await page.getByRole('option', { name: '离线', exact: true }).click()
  await page.clock.fastForward(15001)
  await expect.poll(() => state.calls.length).toBe(2)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  await expect(module).toHaveValue('meta')
  await expect(status).toContainText('离线')
  expect(state.listCalls.at(-1)).toMatchObject({ role: 'worker', status: 'up' })
  expect(state.listCalls.at(-1)).not.toHaveProperty('module_name')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('module_name') === 'meta' && url.searchParams.get('status') === 'down')
  await expect.poll(() => state.listCalls.at(-1)?.module_name).toBe('meta')
})

test('one automatic service cycle advances its selected trend and closing the dialog stops trend reads', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1&role=worker')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  const url = page.url()
  state.setEnd('2026-10-10T00:00:15Z')
  await page.clock.fastForward(15001)
  await expect.poll(() => state.trendCalls.length).toBe(2)
  expect(state.listCalls).toHaveLength(2); expect(state.calls).toHaveLength(2)
  expect(state.trendCalls.at(-1)).toMatchObject({ instance_id: '1', end: '2026-10-10T00:00:15Z', start: '2026-10-09T23:55:15Z' })
  await expect(page.getByTestId('process-trend').getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  expect(page.url()).toBe(url)
  await page.locator('.el-dialog__headerbtn').click()
  await expect(page).toHaveURL(url => !url.searchParams.has('instance_id'))
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  const listCount = state.listCalls.length
  await page.clock.fastForward(15001)
  await expect.poll(() => state.listCalls.length).toBe(listCount + 1)
  expect(state.trendCalls).toHaveLength(2)
})

test('one interval setting restores in both controls and off survives filters, scope and reload', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1&refresh=10')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  for (const id of ['process-refresh', 'process-trend-refresh']) await expect(page.getByTestId(id)).toContainText('每 10 秒')
  await page.getByTestId('process-trend-refresh').click()
  await page.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
  await expect(page).toHaveURL(/refresh=off/)
  await page.locator('.el-dialog__headerbtn').click()
  await page.getByRole('button', { name: '全部实例', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('refresh') === 'off' && url.searchParams.get('status') === 'all')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.reload()
  await expect(page.getByTestId('process-refresh-status')).toContainText('手工刷新')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  const count = state.calls.length
  await page.clock.fastForward(120001)
  expect(state.calls).toHaveLength(count)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect.poll(() => state.calls.length).toBe(count + 1)
  expect(state.listCalls.every(call => !Object.hasOwn(call, 'refresh'))).toBe(true)
})

test('slow cycles preserve the displayed readings, do not overlap and pause discards delayed results', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.hold()
  await page.clock.fastForward(15001)
  await expect.poll(() => state.calls.length).toBe(2)
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  await page.clock.fastForward(60001)
  expect(state.calls).toHaveLength(2)
  await visibility(page, true)
  await expect(page.getByTestId('process-refresh-status')).toContainText('已暂停')
  state.release()
  await page.clock.fastForward(60001)
  expect(state.calls).toHaveLength(2); expect(state.trendCalls).toHaveLength(1)
  state.setEnd('2026-10-10T00:00:30Z')
  await visibility(page, false)
  await expect.poll(() => state.trendCalls.length).toBe(2)
  expect(state.trendCalls.at(-1).end).toBe('2026-10-10T00:00:30Z')
})

test('automatic source failures clear old readings, recover, and denial stops until explicit refresh', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.setMode('unavailable')
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('process-error')).toBeVisible()
  await expect(page.getByTestId('process-list')).not.toContainText('1.50 核')
  await expect(page.getByTestId('process-trend-memory')).toHaveCount(0)
  state.setMode('valid')
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.setTrendMode('denied')
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('process-refresh-status')).toContainText('已停止')
  const count = state.calls.length
  await page.clock.fastForward(60001)
  expect(state.calls).toHaveLength(count)
  state.setTrendMode('valid')
  await page.getByTestId('process-trend').getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('process-refresh-status')).toContainText('已开启')
})

test('a restarted instance cannot become the selected trend and leaving the page cancels the timer', async ({ page }) => {
  const state = await backend(page)
  await page.clock.install()
  await page.goto('/service-resources?instance_id=1')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.replaceInstance(1, 30)
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('process-trend-error')).toContainText('重新选择')
  await expect(page.getByTestId('process-trend-memory')).toHaveCount(0)
  expect(state.trendCalls).toHaveLength(1)
  await page.evaluate(() => { history.pushState({}, '', '/monitoring-targets'); window.dispatchEvent(new PopStateEvent('popstate')) })
  await expect(page.getByTestId('process-list')).toHaveCount(0)
  const count = state.listCalls.length
  await page.clock.fastForward(60001)
  expect(state.listCalls).toHaveLength(count)
})

async function expectTrendChartLayout(page) {
  for (const metric of ['cpu', 'memory']) {
    const container = page.getByTestId(`process-trend-${metric}`)
    await expect(container.locator('canvas')).toBeVisible()
    await expect.poll(async () => container.evaluate(element => {
      const bounds = element.getBoundingClientRect(), canvas = element.querySelector('canvas').getBoundingClientRect()
      return canvas.top >= bounds.top - 1 && canvas.bottom <= bounds.bottom + 1 && canvas.left >= bounds.left - 1 && canvas.right <= bounds.right + 1
    })).toBe(true)
  }
  const cpu = await page.getByTestId('process-trend-cpu').locator('canvas').boundingBox()
  const memoryHeading = await page.getByTestId('process-trend-memory').locator('..').locator('h3').boundingBox()
  expect(cpu.y + cpu.height).toBeLessThanOrEqual(memoryHeading.y)
}

for (const locale of ['zh-cn', 'en']) test(`current process trends restore their scope, draw two charts and keep gaps (${locale})`, async ({ page }, testInfo) => {
  const state = await backend(page, { locale })
  await page.goto('/service-resources')
  await expect(page.getByTestId('process-list')).toContainText(locale === 'en' ? '1.50 cores' : '1.50 核')
  await page.getByTestId('process-list').getByRole('button', { name: locale === 'en' ? 'View trend' : '查看趋势', exact: true }).first().click()
  await expect(page).toHaveURL(url => url.searchParams.get('instance_id') === '1' && !url.searchParams.has('range'))
  const dialog = page.getByTestId('process-trend')
  await expect(dialog).toContainText('native-1')
  for (const metric of ['cpu', 'memory']) await expect(page.getByTestId(`process-trend-${metric}`).locator('canvas')).toBeVisible()
  await expectTrendChartLayout(page)
  await expect(dialog).toContainText(locale === 'en' ? 'missing valid samples remain gaps' : '曲线保留断点')
  expect(state.trendCalls.at(-1)).toMatchObject({ instance_id: '1', start: '2026-10-09T23:55:00Z', end: '2026-10-10T00:00:00Z' })
  const range = dialog.locator('.el-select').first()
  await range.click()
  await page.getByRole('option', { name: locale === 'en' ? 'Last hour' : '近 1 小时', exact: true }).click()
  await expect(page).toHaveURL(/range=1h/)
  await expect(page.getByTestId('process-trend-cpu').locator('canvas')).toBeVisible()
  await page.reload()
  await expect(dialog).toContainText('native-1')
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  expect(state.trendCalls.at(-1).start).toBe('2026-10-09T23:00:00Z')
  await page.setViewportSize({ width: 375, height: 800 })
  await expectTrendChartLayout(page)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath(`process-trend-${locale}-narrow.png`), fullPage: true, animations: 'disabled' })
  const memory = page.getByTestId('process-trend-memory').locator('canvas')
  await memory.scrollIntoViewIfNeeded()
  await expect(memory).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath(`process-trend-${locale}-memory.png`), fullPage: true, animations: 'disabled' })
})

test('trend failures clear charts without clearing a still-authorized instance page', async ({ page }) => {
  const state = await backend(page)
  await page.goto('/service-resources?instance_id=1')
  const dialog = page.getByTestId('process-trend')
  await expect(page.getByTestId('process-trend-cpu').locator('canvas')).toBeVisible()
  for (const mode of ['unavailable', 'wrong-owner']) {
    state.setTrendMode(mode)
    await dialog.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('process-trend-error')).toBeVisible()
    await expect(page.getByTestId('process-trend-cpu')).toHaveCount(0)
    await expect(page.getByTestId('process-list')).toContainText('mac-process-1')
  }
  state.setTrendMode('valid')
  await dialog.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.setTrendMode('denied')
  await dialog.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('process-list')).not.toContainText('mac-process-1')
  await expect(page.getByTestId('process-trend-cpu')).toHaveCount(0)
})

test('a closed trend response cannot overwrite a newly selected instance', async ({ page }) => {
  const state = await backend(page, { trendHeld: true })
  await page.goto('/service-resources?instance_id=1')
  await expect.poll(() => state.trendCalls.length).toBe(1)
  await page.locator('.el-dialog__headerbtn').click()
  await expect(page).toHaveURL(url => !url.searchParams.has('instance_id'))
  await page.getByTestId('process-list').getByRole('button', { name: '查看趋势', exact: true }).nth(1).click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  state.release()
  await expect(page.getByTestId('process-trend')).toContainText('native-2')
  await expect(page.getByTestId('process-trend')).not.toContainText('native-1')
  await expect(page.getByTestId('process-trend-error')).toHaveCount(0)
})

test('revoked identity clears a held process trend before its response arrives', async ({ page }) => {
  const state = await backend(page, { trendHeld: true })
  await page.goto('/service-resources?instance_id=1')
  await expect.poll(() => state.trendCalls.length).toBe(1)
  await setIdentity(page, identity([]))
  state.release()
  await expect(page.getByTestId('process-trend')).toHaveCount(0)
  await expect(page.getByText('当前身份无权查看服务监控。', { exact: false })).toBeVisible()
})

for (const locale of ['zh-cn', 'en']) test(`worker purposes are visible on their own rows and refresh removes outdated labels (${locale})`, async ({ page }, testInfo) => {
  const state = await backend(page, { workerTypes: true, locale })
  await page.goto('/service-resources')
  const list = page.getByTestId('process-list'), rows = list.locator('tbody tr')
  const labels = locale === 'en'
    ? ['Scan worker', 'One-shot sync worker', 'Continuous sync worker', 'Worker', 'Worker', 'Backend']
    : ['扫描任务进程', '单次同步任务进程', '持续同步任务进程', '任务进程', '任务进程', '后端服务']
  await expect(rows).toHaveCount(6)
  for (const [index, label] of labels.entries()) {
    const purpose = rows.nth(index).locator('td').nth(1).locator('p')
    await expect(purpose).toHaveText(label)
    await expect(purpose).toHaveAttribute('title', `typed-${index + 1}`)
  }
  await expect(list).toContainText(locale === 'en' ? '1.50 cores' : '1.50 核')
  await page.screenshot({ path: testInfo.outputPath(`worker-types-${locale}-desktop.png`), fullPage: true })
  await page.setViewportSize({ width: 375, height: 800 })
  await expect(rows.nth(2).locator('td').nth(1)).toContainText(labels[2])
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath(`worker-types-${locale}-narrow.png`), fullPage: true })
  state.setRuntimeName(2, null)
  await page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  await expect(rows.nth(1).locator('td').nth(1).locator('p')).toHaveText(labels[3])
  await expect(list).not.toContainText(labels[1])
  await expect(rows.nth(2).locator('td').nth(1).locator('p')).toHaveText(labels[2])
  await page.reload()
  await expect(rows.nth(1).locator('td').nth(1).locator('p')).toHaveText(labels[3])
})

test('service resources use current System instances, local explanations and correct process units', async ({ page }) => {
  const state = await backend(page)
  await page.goto('/service-resources')
  const list = page.getByTestId('process-list')
  await expect(list).toContainText('1.50 核')
  await expect(list).toContainText('0.00 核')
  await expect(list).toContainText('1.00 GiB')
  await expect(list).toContainText('1小时')
  await expect(list).toContainText('未关联主机')
  await expect(list).toContainText('采样时间')
  await expect(list).toContainText('1 核 ≈ 占满一个逻辑核，可超过 1 核')
  expect(state.calls).toEqual(['1,2'])
  state.setMode('warmup')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list).toContainText('等待完整一分钟有效采样')
  await expect(list).toContainText('1.00 GiB')
})

test('failed evidence clears metrics but keeps the authorized instance list; owner denial clears both', async ({ page }) => {
  const state = await backend(page)
  await page.goto('/service-resources')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  for (const mode of ['unavailable', 'wrong-owner']) {
    state.setMode(mode)
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('process-error')).toBeVisible()
    await expect(page.getByTestId('process-list')).toContainText('mac-process-1')
    await expect(page.getByTestId('process-list')).not.toContainText('1.50 核')
  }
  state.setMode('denied')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('process-list')).not.toContainText('mac-process-1')
})

test('revoked permissions cannot receive a delayed service response', async ({ page }) => {
  const state = await backend(page, { held: true })
  await page.goto('/service-resources')
  await expect(page.getByTestId('process-list')).toContainText('mac-process-1')
  await expect.poll(() => state.calls.length).toBe(1)
  await setIdentity(page, identity([]))
  state.release()
  await expect(page.getByText('当前身份无权查看服务监控。', { exact: false })).toBeVisible()
  await expect(page.getByTestId('process-list')).toHaveCount(0)
})

test('service filters and paging restore the exact System scope without manual targets', async ({ page }) => {
  const state = await backend(page, { locale: 'en' })
  await page.goto('/service-resources?module_name=monitor&role=worker&status=up')
  await expect(page.getByTestId('process-list')).toContainText('1.50 cores')
  expect(state.listCalls[0]).toMatchObject({ module_name: 'monitor', role: 'worker', status: 'up', page_size: '20' })
  await page.locator('.el-pagination button.btn-next').click()
  await expect(page).toHaveURL(/page=2/)
  await expect(page.getByTestId('process-list')).not.toContainText('mac-process-1')
  await page.reload()
  expect(state.listCalls.at(-1).page).toBe('2')
  expect(state.calls).toHaveLength(1)
})

test('default scope finds declared online sources beyond historical pages and retains failures', async ({ page }, testInfo) => {
  const state = await backend(page, { scopes: true })
  state.setMode('failed')
  await page.goto('/service-resources')
  const list = page.getByTestId('process-list')
  await expect(list).toContainText('mac-process-1')
  await expect(list).toContainText('mac-process-2')
  await expect(list).toContainText('采集失败')
  const headers = await list.locator('thead th').allTextContents()
  expect(headers.findIndex(text => text.includes('采集状态'))).toBe(headers.findIndex(text => text.includes('在线状态')) + 1)
  await expect(list).not.toContainText('history-')
  await expect(list).not.toContainText('not-connected')
  expect(state.listCalls.at(-1)).toMatchObject({ status: 'up', process_metrics_declared: 'true' })
  await expect(page.locator('.el-pagination')).toContainText('共 2 条')
  await page.getByRole('button', { name: '全部实例', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('status') === 'all' && url.searchParams.get('process_metrics_declared') === 'all')
  await expect(list).toContainText('history-0')
  await expect(page.locator('.el-pagination')).toContainText('共 27 条')
  expect(state.listCalls.at(-1)).not.toHaveProperty('status')
  expect(state.listCalls.at(-1)).not.toHaveProperty('process_metrics_declared')
  await page.locator('.el-pagination button.btn-next').click()
  await expect(list).toContainText('not-connected')
  await expect(list).toContainText('未接入采集')
  await page.reload()
  await expect(list).toContainText('not-connected')
  expect(state.listCalls.at(-1).page).toBe('2')
  await page.getByRole('button', { name: '在线且已启用采集', exact: true }).click()
  await expect(page).toHaveURL(url => !url.search)
  await expect(list).toContainText('mac-process-2')
  await expect(list).toContainText('采集失败')
  expect(state.listCalls.at(-1).page).toBe('1')
  await page.screenshot({ path: testInfo.outputPath('service-scope-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 375, height: 800 })
  await expect(page.getByRole('button', { name: '全部实例', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('service-scope-narrow.png'), fullPage: true })
})

test('collection filter is explicit, restores in English and leaves the all-instances action available on empty results', async ({ page }) => {
  const state = await backend(page, { scopes: true, locale: 'en' })
  await page.goto('/service-resources?status=down&process_metrics_declared=false')
  await expect(page.getByTestId('process-list')).toContainText('No matching service instances')
  await expect(page.locator('.collection-filter')).toContainText('Enabled does not mean collection succeeded')
  await page.reload()
  expect(state.listCalls.at(-1)).toMatchObject({ status: 'down', process_metrics_declared: 'false' })
  await expect(page.locator('.collection-filter .el-select')).toContainText('Collection not enabled')
  await page.locator('.el-select:has(input[aria-label="Online status"])').click()
  await page.getByRole('option', { name: 'Online', exact: true }).click()
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.getByTestId('process-list')).toContainText('not-connected')
  expect(state.listCalls.at(-1)).toMatchObject({ status: 'up', process_metrics_declared: 'false', page: '1' })
  await page.getByRole('button', { name: 'All instances', exact: true }).click()
  await expect(page.getByTestId('process-list')).toContainText('history-0')
})


for (const locale of ['zh-cn', 'en']) test(`registered address filter survives trend and reload, and reset restores current scope (${locale})`, async ({ page }, info) => {
  const state = await backend(page, { locale, instances: [
    { id: 1, role: 'backend', registered_host: 'db.internal', module_url: 'http://db.internal:8180' },
    { id: 2, role: 'backend', registered_host: 'db.internal', module_url: 'http://db.internal:8190' },
    { id: 3, role: 'backend', registered_host: 'other.internal', runtime_hostname: 'db.internal', module_url: 'http://other.internal:8180' },
    { id: 4, role: 'worker', registered_host: '', runtime_hostname: 'db.internal', module_url: '', health_check_url: '' }
  ] })
  const address = page.getByRole('textbox', { name: locale === 'en' ? 'Registered hostname or IP (exact match)' : '登记主机名或 IP（精确匹配）', exact: true })
  const search = page.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })
  const reset = page.getByRole('button', { name: locale === 'en' ? 'Reset' : '重置', exact: true })
  await page.goto('/service-resources?refresh=off')
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(4)
  await address.fill('  DB.INTERNAL  ')
  await search.click()
  await expect(page).toHaveURL(/registered_host=db.internal/)
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(2)
  await expect(address).toHaveValue('db.internal')
  expect(state.listCalls.at(-1).registered_host).toBe('db.internal')
  await expect.poll(() => state.calls.at(-1)).toBe('1,2')
  await page.screenshot({ path: info.outputPath(`service-address-${locale}.png`), fullPage: true })
  await page.getByTestId('process-list').getByRole('button', { name: locale === 'en' ? 'View trend' : '查看趋势', exact: true }).first().click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  await page.reload()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  await expect(address).toHaveValue('db.internal')
  expect(state.listCalls.at(-1).registered_host).toBe('db.internal')
  expect(state.trendCalls.at(-1)).not.toHaveProperty('registered_host')
  await page.locator('.el-dialog__headerbtn').click()
  await reset.click()
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  await expect(address).toHaveValue('')
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(4)
  expect(state.listCalls.at(-1)).toMatchObject({ page: '1', status: 'up', process_metrics_declared: 'true' })
  expect(state.listCalls.at(-1)).not.toHaveProperty('registered_host')
  // Same canonical route still clears an unsubmitted draft.
  await address.fill('not-submitted.internal')
  await reset.click()
  await expect(address).toHaveValue('')
})

test('automatic refresh retains the submitted address and does not apply an address draft', async ({ page }) => {
  const state = await backend(page, { instances: [{ id: 1, registered_host: 'db.internal' }, { id: 2, registered_host: 'other.internal' }] })
  await page.clock.install()
  await page.goto('/service-resources?registered_host=db.internal')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  const address = page.getByRole('textbox', { name: '登记主机名或 IP（精确匹配）', exact: true })
  await address.fill('other.internal')
  await page.clock.fastForward(15001)
  await expect.poll(() => state.calls.length).toBe(2)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  expect(state.listCalls.at(-1).registered_host).toBe('db.internal')
  expect(state.calls.at(-1)).toBe('1')
  await expect(address).toHaveValue('other.internal')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => state.calls.at(-1)).toBe('2')
  await expect(page).toHaveURL(/registered_host=other.internal/)
})


test('relative instance time advances per request, keeps draft and pagination, and stays out of process trends', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-10-22T10:00:00Z') })
  const state = await backend(page, { instances: Array.from({ length: 21 }, (_, i) => ({ id: i + 1, registered_at: '2026-10-22T09:59:00Z' })) })
  await page.goto('/service-resources?time_period=1h')
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(20)
  const first = state.listCalls.at(-1)
  expect(first.status).toBe('up')
  expect(Date.parse(first.time_to) - Date.parse(first.time_from)).toBe(3600000)
  expect(Date.parse(first.time_to)).toBeGreaterThanOrEqual(Date.parse('2026-10-22T10:00:00Z'))
  expect(first).not.toHaveProperty('time_period')
  const period = page.locator('.runtime-instance-time-filter .el-select').nth(1)
  await period.click()
  await page.getByRole('option', { name: '近 24 小时', exact: true }).click()
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  await page.clock.fastForward(15001)
  await expect.poll(() => state.listCalls.length).toBe(2)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  expect(Date.parse(state.listCalls.at(-1).time_from)).toBeGreaterThan(Date.parse(first.time_from))
  expect(Date.parse(state.listCalls.at(-1).time_to) - Date.parse(state.listCalls.at(-1).time_from)).toBe(3600000)
  await expect(period).toContainText('近 24 小时')
  await expect(page).toHaveURL(/time_period=1h$/)
  await page.clock.resume()
  await page.locator('.el-pagination button.btn-next').click()
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(1)
  expect(state.listCalls.at(-1).page).toBe('2')
  await expect(period).toContainText('近 1 小时')
  await page.getByTestId('process-list').getByRole('button', { name: '查看趋势', exact: true }).click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  expect(state.trendCalls.at(-1)).not.toHaveProperty('time_from')
  expect(state.trendCalls.at(-1)).not.toHaveProperty('time_basis')
  await page.reload()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  expect(state.listCalls.at(-1).page).toBe('2')
  await expect(page).toHaveURL(url => url.searchParams.get('time_period') === '1h' && url.searchParams.get('page') === '2')
})

for (const locale of ['zh-cn', 'en']) test(`custom offline time is fixed, half-open, current-only and resettable (${locale})`, async ({ page }, info) => {
  const state = await backend(page, { locale, instances: [
    { id: 1, status: 'down', registered_at: '2026-10-01T00:00:00Z', stopped_at: '2026-10-22T10:15:00Z' },
    { id: 2, status: 'down', registered_at: '2026-10-01T00:00:00Z', stopped_at: '2026-10-22T11:00:00Z' },
    { id: 3, status: 'up', registered_at: '2026-10-22T10:15:00Z', stopped_at: null },
    { id: 4, status: 'down', registered_at: '2026-10-01T00:00:00Z', stopped_at: '2026-10-22T10:00:00Z' }
  ] })
  const from = '2026-10-22T18:00:00+08:00', to = '2026-10-22T19:00:00+08:00'
  await page.goto('/service-resources?' + new URLSearchParams({ status: 'all', refresh: 'off', time_basis: 'offline', time_period: 'custom', time_from: from, time_to: to }))
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(2)
  await expect.poll(() => state.calls.at(-1)).toBe('1,4')
  const first = state.listCalls.at(-1)
  expect(first).toMatchObject({ time_basis: 'offline', time_from: '2026-10-22T10:00:00.000Z', time_to: '2026-10-22T11:00:00.000Z' })
  expect(first).not.toHaveProperty('status')
  await expect(page.locator('.runtime-instance-time-hint').first()).toContainText(locale === 'en' ? 'recovered instances' : '恢复后退出结果')
  await page.reload()
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(2)
  expect(state.listCalls.at(-1)).toEqual(first)
  state.setStatus(1, 'up')
  await page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  await expect.poll(() => state.calls.at(-1)).toBe('4')
  expect(state.listCalls.at(-1)).toEqual(first)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath(`service-time-${locale}.png`), fullPage: true })
  await page.getByRole('button', { name: locale === 'en' ? 'Reset' : '重置', exact: true }).click()
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(2)
  expect(state.listCalls.at(-1)).toMatchObject({ status: 'up' })
  expect(state.listCalls.at(-1)).not.toHaveProperty('time_basis')
  expect(state.listCalls.at(-1)).not.toHaveProperty('time_from')
  const control = page.locator('.runtime-instance-time-filter .el-select').nth(1)
  await control.click()
  await page.getByRole('option', { name: locale === 'en' ? 'Custom range' : '自定义时间', exact: true }).click()
  const count = state.listCalls.length
  await expect(page.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })).toBeDisabled()
  await expect(page.locator('.runtime-instance-time-filter').getByRole('status')).toContainText(locale === 'en' ? 'Select both' : '请选择完整')
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  expect(state.listCalls).toHaveLength(count)
  const dates = page.locator('.runtime-instance-time-filter .el-range-input')
  await dates.nth(0).fill('2026-10-22 10:00:00')
  await dates.nth(0).press('Tab')
  await dates.nth(1).fill('2026-10-22 11:00:00')
  await dates.nth(1).press('Enter')
  const submit = page.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })
  await expect(submit).toBeEnabled()
  await submit.click()
  const expected = await page.evaluate(() => ({ time_from: new Date(2026, 9, 22, 10).toISOString(), time_to: new Date(2026, 9, 22, 11).toISOString() }))
  await expect.poll(() => state.listCalls.at(-1)).toMatchObject(expected)
  await expect(page).toHaveURL(url => url.searchParams.get('time_period') === 'custom' && url.searchParams.get('time_from') === expected.time_from)
})

for (const locale of ['zh-cn', 'en']) test(`service stop reasons preserve System intersections, paging and recovery (${locale})`, async ({ page }, testInfo) => {
  const en = locale === 'en'
  const graceful = en ? 'Graceful Exit' : '正常退出', expired = en ? 'Lease expired; possible unexpected exit' : '租约超时，疑似异常退出'
  const all = en ? 'All instances' : '全部实例', search = en ? 'Search' : '搜索'
  const state = await backend(page, { locale, instances: [
    { id: 1, stop_reason: 'lease_expired', registered_at: '2026-10-22T10:00:00Z' },
    { id: 2, status: 'down', stop_reason: 'graceful', stopped_at: '2026-10-22T10:30:00Z' },
    ...Array.from({ length: 23 }, (_, index) => ({ id: index + 3, status: 'down', stop_reason: 'lease_expired', stopped_at: '2026-10-22T10:30:00Z', process_metrics_declared: index !== 22 })),
    { id: 26, status: 'down', stop_reason: 'future_reason', stopped_at: '2026-10-22T10:30:00Z' }
  ] })
  await page.setViewportSize({ width: 390, height: 900 })
  await page.goto('/service-resources?status=all&time_basis=offline&refresh=off&page=2')
  const reason = page.locator('.process-filters .el-select').filter({ has: page.locator(`input[aria-label="${en ? 'Stop Reason' : '下线原因'}"]`) })
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(4)
  const previous = state.listCalls.length
  await reason.click()
  await page.getByRole('option', { name: graceful, exact: true }).click()
  await expect(reason).toContainText(graceful)
  expect(state.listCalls.at(-1)).not.toHaveProperty('stop_reason')
  expect(state.listCalls).toHaveLength(previous)
  await page.getByRole('button', { name: search, exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('stop_reason') === 'graceful' && url.searchParams.get('status') === 'all' && url.searchParams.get('time_basis') === 'offline' && !url.searchParams.has('page'))
  await expect(page.getByTestId('process-stop-reason')).toHaveText([graceful])
  expect(state.calls.at(-1)).toBe('2')
  expect(state.trendCalls).toHaveLength(0)
  await expect(page.getByRole('button', { name: en ? 'View trend' : '查看趋势', exact: true })).toBeDisabled()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(page.locator('.el-select__popper:visible')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath(`service-stop-reason-${locale}.png`), fullPage: true })
  await reason.click()
  await page.getByRole('option', { name: expired, exact: true }).click()
  await page.getByRole('button', { name: all, exact: true }).click()
  await expect(page.getByTestId('process-stop-reason')).toHaveText(Array(20).fill(expired))
  expect(state.listCalls.at(-1)).toMatchObject({ stop_reason: 'lease_expired', time_basis: 'offline', page: '1' })
  expect(state.listCalls.at(-1)).not.toHaveProperty('status')
  expect(state.listCalls.at(-1)).not.toHaveProperty('process_metrics_declared')
  await expect(page.getByTestId('process-list')).toContainText(en ? 'does not prove that the process exited' : '不能据此断定进程已经退出')
  await page.locator('.el-pagination button.btn-next').click()
  await expect.poll(() => state.calls.at(-1)).toBe('23,24,25')
  await page.reload()
  await expect(page.getByTestId('process-stop-reason')).toHaveText(Array(3).fill(expired))
  await expect(reason).toContainText(expired)
  expect(state.listCalls.at(-1)).toMatchObject({ stop_reason: 'lease_expired', page: '2', time_basis: 'offline' })
  state.setStatus(23, 'up')
  await page.getByRole('button', { name: en ? 'Refresh' : '刷新', exact: true }).click()
  await expect.poll(() => state.calls.at(-1)).toBe('24,25')
  const summaries = state.calls.length
  await page.getByRole('button', { name: en ? 'Online with collection enabled' : '在线且已启用采集', exact: true }).click()
  await expect(page.getByTestId('process-stop-reason')).toHaveCount(0)
  expect(state.calls).toHaveLength(summaries)
  expect(state.listCalls.at(-1)).toMatchObject({ status: 'up', stop_reason: 'lease_expired', time_basis: 'offline', process_metrics_declared: 'true', page: '1' })
  await page.getByRole('button', { name: en ? 'Reset' : '重置', exact: true }).click()
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  await expect(page.getByTestId('process-stop-reason')).toHaveText(['—', '—'])
  await expect(reason).toContainText(en ? 'All stop reasons' : '全部下线原因')
  expect(state.listCalls.at(-1)).not.toHaveProperty('stop_reason')
})

test('automatic service refresh keeps the applied reason separate from its draft', async ({ page }) => {
  const state = await backend(page, { instances: [
    { id: 1, status: 'down', stop_reason: 'lease_expired' },
    { id: 2, status: 'down', stop_reason: 'graceful' }
  ] })
  await page.clock.install()
  await page.goto('/service-resources?status=all&stop_reason=lease_expired&refresh=10')
  await expect(page.getByTestId('process-stop-reason')).toHaveText(['租约超时，疑似异常退出'])
  const reason = page.locator('.process-filters .el-select:has(input[aria-label="下线原因"])')
  await reason.click()
  await page.getByRole('option', { name: '正常退出', exact: true }).click()
  const before = state.listCalls.length
  await page.clock.fastForward(10001)
  await expect.poll(() => state.listCalls.length).toBeGreaterThan(before)
  expect(state.listCalls.at(-1).stop_reason).toBe('lease_expired')
  await expect(reason).toContainText('正常退出')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => state.calls.at(-1)).toBe('2')
  await expect(page.getByTestId('process-stop-reason')).toHaveText(['正常退出'])
  state.setStatus(2, 'up')
  const beforeRecovery = state.listCalls.length
  await page.clock.fastForward(10001)
  await expect.poll(() => state.listCalls.length).toBeGreaterThan(beforeRecovery)
  await expect(page.getByTestId('process-stop-reason')).toHaveCount(0)
})


for (const locale of ['zh-cn', 'en']) test(`host IP matches declared hosts including endpoint-free workers and survives paging, trend and reset (${locale})`, async ({ page }, info) => {
  const en = locale === 'en', ip = '192.0.2.7'
  const state = await backend(page, { locale, instances: [
    ...Array.from({ length: 23 }, (_, i) => ({ id: i + 1, host_node_ips: [ip], registered_host: '', module_url: '' })),
    { id: 24, host_node_ips: ['192.0.2.70'], registered_host: ip, runtime_hostname: ip },
    { id: 25, host_node_ips: [ip], status: 'down' },
    { id: 26, host_node_ips: [ip], process_metrics_declared: false }
  ] })
  await page.goto('/service-resources?module_name=monitor&role=worker&refresh=off')
  const input = page.getByRole('textbox', { name: en ? 'Host IP (exact match)' : '承载主机 IP（精确匹配）', exact: true })
  const list = page.getByTestId('process-list')
  await expect(list.locator('.el-table__body tr')).toHaveCount(20)
  await input.fill(`  ${ip}  `)
  await page.getByRole('button', { name: en ? 'Search' : '搜索', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('node_ip') === ip && !url.searchParams.has('page'))
  await expect(input).toHaveValue(ip)
  await expect(page.locator('.el-pagination__total')).toContainText('23')
  await expect.poll(() => state.calls.at(-1)).toBe(Array.from({ length: 20 }, (_, i) => i + 1).join(','))
  expect(state.hostReads).toEqual([])
  await page.locator('.el-pagination .btn-next').click()
  await expect.poll(() => state.calls.at(-1)).toBe('21,22,23')
  expect(state.listCalls.at(-1)).toMatchObject({ page: '2', node_ip: ip, module_name: 'monitor', role: 'worker' })
  await page.reload()
  await expect(list.locator('.el-table__body tr')).toHaveCount(3)
  await expect(input).toHaveValue(ip)
  await list.getByRole('button', { name: en ? 'View trend' : '查看趋势', exact: true }).first().click()
  await expect(page.getByTestId('process-trend-memory').locator('canvas')).toBeVisible()
  expect(state.trendCalls.at(-1)).not.toHaveProperty('node_ip')
  await page.locator('.el-dialog__headerbtn').click()
  await page.getByRole('button', { name: en ? 'All instances' : '全部实例', exact: true }).click()
  await expect(page.locator('.el-pagination__total')).toContainText('25')
  expect(state.listCalls.at(-1).node_ip).toBe(ip)
  await page.getByRole('button', { name: en ? 'Reset' : '重置', exact: true }).click()
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  await expect(input).toHaveValue('')
  await expect(page.locator('.el-pagination__total')).toContainText('24')
  expect(state.listCalls.at(-1)).not.toHaveProperty('node_ip')
  await input.fill('not-submitted')
  await page.getByRole('button', { name: en ? 'Reset' : '重置', exact: true }).click()
  await expect(input).toHaveValue('')
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(input).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath(`service-host-ip-${locale}.png`), fullPage: true })
})

test('System host IP matching accepts IPv6 and mapped IPv4, rejects invalid literals without broadening scope', async ({ page }) => {
  const state = await backend(page, { instances: [
    { id: 1, host_node_ips: ['2001:db8::7'] },
    { id: 2, host_node_ips: ['192.0.2.7'] },
    { id: 3, host_node_ips: [], registered_host: '192.0.2.7', runtime_hostname: '2001:db8::7' }
  ] })
  for (const [node_ip, id] of [['2001:0DB8:0:0:0:0:0:7', '1'], ['::ffff:192.0.2.7', '2']]) {
    await page.goto('/service-resources?' + new URLSearchParams({ node_ip, refresh: 'off' }))
    await expect.poll(() => state.calls.at(-1)).toBe(id)
    await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(1)
    expect(state.listCalls.at(-1).node_ip).toBe(node_ip)
  }
  for (const node_ip of ['host.internal', '192.0.2.7:9100', '192.0.2.0/24', 'fe80::1%en0']) {
    const count = state.calls.length
    await page.goto('/service-resources?' + new URLSearchParams({ node_ip, refresh: 'off' }))
    await expect(page.getByTestId('process-error')).toContainText('查询条件无效')
    await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(0)
    expect(state.listCalls.at(-1).node_ip).toBe(node_ip)
    expect(state.calls).toHaveLength(count)
  }
  await page.goto('/service-resources?node_ip=192.0.2.7&node_ip=192.0.2.70&refresh=off')
  await expect(page).toHaveURL(/service-resources\?refresh=off$/)
  await expect(page.getByTestId('process-list').locator('.el-table__body tr')).toHaveCount(3)
  expect(state.listCalls.at(-1)).not.toHaveProperty('node_ip')
})

test('host IP auto refresh uses only submitted values and combines endpoint, time and reason conditions', async ({ page }) => {
  const state = await backend(page, { instances: [
    { id: 1, host_node_ips: ['192.0.2.7'], registered_host: 'db.internal', status: 'down', stop_reason: 'graceful', stopped_at: '2026-10-22T10:30:00Z' },
    { id: 2, host_node_ips: ['192.0.2.8'], registered_host: 'db.internal', status: 'down', stop_reason: 'graceful', stopped_at: '2026-10-22T10:30:00Z' },
    { id: 3, host_node_ips: ['192.0.2.7'], registered_host: 'other.internal', status: 'down', stop_reason: 'graceful', stopped_at: '2026-10-22T10:30:00Z' }
  ] })
  await page.clock.install()
  const query = new URLSearchParams({ node_ip: '192.0.2.7', registered_host: 'db.internal', status: 'down', stop_reason: 'graceful', time_basis: 'offline', time_period: 'custom', time_from: '2026-10-22T10:00:00.000Z', time_to: '2026-10-22T11:00:00.000Z', refresh: '10' })
  await page.goto('/service-resources?' + query)
  await expect.poll(() => state.calls.at(-1)).toBe('1')
  const input = page.getByRole('textbox', { name: '承载主机 IP（精确匹配）', exact: true })
  await input.fill('192.0.2.8')
  const count = state.calls.length
  await page.clock.fastForward(10001)
  await expect.poll(() => state.calls.length).toBe(count + 1)
  expect(state.listCalls.at(-1).node_ip).toBe('192.0.2.7')
  await expect(input).toHaveValue('192.0.2.8')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => state.calls.at(-1)).toBe('2')
  expect(state.listCalls.at(-1)).toMatchObject({ node_ip: '192.0.2.8', registered_host: 'db.internal', status: 'down', stop_reason: 'graceful', time_basis: 'offline' })
})
