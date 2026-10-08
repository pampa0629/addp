import { expect, test } from '@playwright/test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, node, resourceBackend, resourcePermissions, secondNode, setIdentity } from './node-resources.fixture'

test('resource quantities use binary capacity and elapsed time while load is not a percent', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(page.getByTestId('resource-memoryTotal')).toContainText('16.00 GiB')
  await expect(page.getByTestId('resource-uptime')).toContainText('1小时')
  const load = page.getByTestId('resource-load1m')
  await expect(load).toContainText('系统平均负载（1 分钟）')
  await expect(load.locator('strong')).toHaveText('0.00')
  await expect(page.getByText('系统平均负载：运行或等待 CPU，以及不可中断等待（常见于 I/O）的任务数量平均值；不是百分比，也不是 CPU 或内存使用率。', { exact: true })).toBeVisible()
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: '内存总量', exact: true }).click()
  await page.getByText('查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('16.00 GiB')
  expect(state.reads.find(item => item.path.endsWith('resource_trends') && item.query.metrics === 'node.memory.total_bytes')).toBeTruthy()
})

test('lists owner nodes without fanout and opens nine metrics with server-anchored trends', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/node-resources?page=2&search=节点')
  await expect(page.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  expect(state.reads.filter(item => item.path.includes('resource_'))).toHaveLength(0)
  await page.getByRole('button', { name: '查看资源' }).first().click()
  await expect(page).toHaveURL(new RegExp(`/node-resources/${node}\\?page=2&search=`))
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-memoryAvailable')).toContainText('陈旧')
  await expect(page.getByTestId('resource-memoryAvailable')).toContainText('—')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const instant = state.reads.find(item => item.path.endsWith('resource_observations'))
  expect(instant.query.metrics.split(',')).toHaveLength(9)
  const trend = state.reads.find(item => item.path.endsWith('resource_trends'))
  expect(trend.query).toMatchObject({ node_id: node, metrics: 'node.memory.used_percent', start: '2026-10-06T23:00:00.000Z', end: '2026-10-07T00:00:00.000Z' })
  await page.getByText('查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('缺失')
  await page.screenshot({ path: join(tmpdir(), 'addp-node-resources-desktop.png'), animations: 'disabled' })
  await page.getByRole('button', { name: '返回主机列表' }).click()
  await expect(page.getByTestId('resource-search')).toHaveValue('节点')
})
test('canonical detail restores metric and range on reload; no endpoint or token enters URL', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?range=7d&metric=node.load.average_1m&page=02&endpoint=secret`)
  await expect(page).toHaveURL(new RegExp(`/node-resources/${node}\\?range=7d&metric=node.load.average_1m$`))
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(state.reads.find(item => item.path.endsWith('resource_trends')).query.start).toBe('2026-09-30T00:00:00.000Z')
  await page.reload()
  await expect(page.getByTestId('resource-metric')).toContainText('系统平均负载（1 分钟）')
  await expect(page.getByTestId('resource-range')).toContainText('近 7 天')
})
for (const mode of ['disabled', 'unconfigured', 'unavailable', 'timeout', 'budget', 'busy']) test(`${mode} refresh clears old metrics and has a distinct state`, async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.mode = mode
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  expect(state.reads.filter(item => item.path.endsWith('resource_trends'))).toHaveLength(1)
})
for (const [name, actor] of [
  ['tenant', identity(resourcePermissions, { type: 'tenant', tenant_id: '9' })],
  ['service', identity(resourcePermissions, { type: 'platform' }, 'service_principal')],
  ['delegated', identity(resourcePermissions, { type: 'platform' }, 'user', { id: 'delegation' })],
  ['missing node permission', identity(['monitor.resource_observation.read'])],
  ['missing metric permission', identity(['platform.host_node.read'])]
]) test(`${name} cannot request node or resource data`, async ({ page }) => {
  const state = await resourceBackend(page, { identity: actor })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})
test('identity change clears the node and discards late observations', async ({ page }) => {
  const state = await resourceBackend(page, { holdInstant: true })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByRole('heading', { name: '节点甲' })).toBeVisible()
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '8' }))
  state.release()
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await setIdentity(page, identity(resourcePermissions))
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(state.reads.filter(item => item.path.endsWith('resource_trends'))).toHaveLength(1)
})
test('navigation to another node discards the previous delayed response', async ({ page }) => {
  const state = await resourceBackend(page, { holdInstant: true })
  await page.goto(`/node-resources/${node}`)
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_observations')).length).toBe(1)
  await page.getByRole('button', { name: '返回主机列表' }).click()
  await page.getByRole('button', { name: '查看资源' }).nth(1).click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.release()
  await expect(page.getByRole('heading', { name: '节点乙' })).toBeVisible()
  expect(state.reads.filter(item => item.path.endsWith('resource_trends')).map(item => item.query.node_id)).toEqual([secondNode])
})
test('trend failure clears the previous chart while current readings remain independently valid', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.trendMode = 'unavailable'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
})
test('disconnected points stay empty; English narrow layout has no horizontal overflow', async ({ page }) => {
  await resourceBackend(page, { locale: 'en', mode: 'disconnected' })
  await page.setViewportSize({ width: 620, height: 800 })
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-cores')).toContainText('Not connected')
  await expect(page.getByTestId('resource-cores')).toContainText('—')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await page.screenshot({ path: join(tmpdir(), 'addp-node-resources-narrow.png'), animations: 'disabled' })
})
test('invalid node or mismatched response is an error instead of another node', async ({ page }) => {
  const state = await resourceBackend(page, { mode: 'invalid' })
  await page.goto('/node-resources/not-a-uuid')
  await expect(page.getByTestId('resource-error')).toContainText('主机标识无效')
  expect(state.reads).toHaveLength(0)
  await page.goto(`/node-resources/${node}`)
  await expect(page.getByTestId('resource-error')).toContainText('资源响应不完整')
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
})


const resourceReads = state => state.reads.filter(item => item.path.endsWith('resource_observations') && item.query.metrics.startsWith('node.cpu.logical_cores'))
const visibleChart = page => expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
async function visibility(page, hidden) {
  await page.evaluate(hidden => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: hidden })
    document.dispatchEvent(new Event('visibilitychange'))
  }, hidden)
}
async function chooseRefresh(page, name) {
  await page.getByTestId('resource-refresh').click()
  await page.getByRole('option', { name, exact: true }).click()
  await visibleChart(page)
}
test('automatic refresh advances the server window without changing URL or fanout on the list', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}`)
  await visibleChart(page)
  const url = page.url()
  state.serverEnd = '2026-10-07T00:00:30Z'
  await page.clock.fastForward(15001)
  await expect.poll(() => resourceReads(state).length).toBe(2)
  await visibleChart(page)
  // Observations arrive before their anchored trend; an existing canvas is not completion evidence.
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query).toMatchObject({ start: '2026-10-06T23:00:30.000Z', end: '2026-10-07T00:00:30.000Z' })
  expect(page.url()).toBe(url)
  await page.getByRole('button', { name: '返回主机列表' }).click()
  await expect(page.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(2)
})
test('refresh interval and off survive reload and leave no timers after disabling', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}?refresh=10`)
  await visibleChart(page)
  await expect(page.getByTestId('resource-refresh')).toContainText('每 10 秒')
  await page.reload()
  await visibleChart(page)
  await page.clock.fastForward(10001)
  await expect.poll(() => resourceReads(state).length).toBe(3)
  await visibleChart(page)
  await chooseRefresh(page, '关闭自动刷新')
  await expect(page).toHaveURL(new RegExp('\\?refresh=off$'))
  await page.reload()
  await visibleChart(page)
  const count = resourceReads(state).length
  await page.clock.fastForward(120001)
  expect(resourceReads(state)).toHaveLength(count)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await visibleChart(page)
  expect(resourceReads(state)).toHaveLength(count + 1)
})
test('hidden details pause, cancel late observations and resume once immediately', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}`)
  await visibleChart(page)
  state.hold()
  await page.clock.fastForward(15001)
  await expect.poll(() => resourceReads(state).length).toBe(2)
  await visibility(page, true)
  await expect(page.getByTestId('resource-refresh-status')).toContainText('已暂停')
  state.serverEnd = '2026-10-07T00:01:00Z'
  state.release()
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(2)
  expect(state.reads.filter(item => item.path.endsWith('resource_trends'))).toHaveLength(1)
  await visibility(page, false)
  await expect.poll(() => resourceReads(state).length).toBe(3)
  await visibleChart(page)
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query.end).toBe('2026-10-07T00:01:00.000Z')
})
test('slow automatic requests do not overlap and failures clear old readings before recovery', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}`)
  await visibleChart(page)
  state.hold()
  state.mode = 'unavailable'
  await page.clock.fastForward(15001)
  await expect.poll(() => resourceReads(state).length).toBe(2)
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(2)
  state.release()
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  state.mode = ''
  await page.clock.fastForward(15001)
  await expect.poll(() => resourceReads(state).length).toBe(3)
  await visibleChart(page)
})
test('automatic permission denial stops retrying and explicit refresh rechecks permission', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}`)
  await visibleChart(page)
  state.mode = 'denied'
  await page.clock.fastForward(15001)
  await expect(page.getByTestId('resource-error')).toBeVisible()
  await expect(page.getByTestId('resource-refresh-status')).toContainText('已停止')
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(2)
  state.mode = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await visibleChart(page)
  expect(resourceReads(state)).toHaveLength(3)
})
test('context revocation and component unmount stop future automatic reads', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.clock.install()
  await page.goto(`/node-resources/${node}`)
  await visibleChart(page)
  await setIdentity(page, identity(resourcePermissions, { type: 'tenant', tenant_id: '9' }))
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(1)
  await setIdentity(page, identity(resourcePermissions))
  await visibleChart(page)
  await page.evaluate(() => { history.pushState({}, '', '/monitoring-targets'); window.dispatchEvent(new PopStateEvent('popstate')) })
  await expect(page.getByTestId('node-resources')).toHaveCount(0)
  const count = resourceReads(state).length
  await page.clock.fastForward(60001)
  expect(resourceReads(state)).toHaveLength(count)
})


test('CPU minute warmup stays empty while gauges work; recovery restores valid zero and its trend', async ({ page }) => {
  const state = await resourceBackend(page, { mode: 'cpu-warmup' })
  await page.goto(`/node-resources/${node}?metric=node.cpu.busy_percent&refresh=off`)
  await expect(page.getByTestId('resource-cpuBusy')).toContainText('缺失')
  await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('—')
  await page.getByTestId('resource-cpuBusy').getByText('采样说明', { exact: true }).click()
  await expect(page.getByTestId('resource-cpuBusy')).toContainText('最近一分钟')
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-metric')).toContainText('CPU 忙碌率（1 分钟）')
  state.mode = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-cpuBusy')).toContainText('有效')
  await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('0.00 %')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await page.reload()
  await expect(page.getByTestId('resource-metric')).toContainText('CPU 忙碌率（1 分钟）')
  expect(state.reads.filter(item => item.path.endsWith('resource_trends')).every(item => item.query.metrics === 'node.cpu.busy_percent')).toBe(true)
})

test('filesystem table selects an exact mount, restores its trend and keeps one bounded refresh cycle', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  await visibleChart(page)
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('/data')
  await page.getByTestId('resource-filesystem-table').getByRole('button', { name: '查看趋势' }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('device') === '/dev/fixture' && url.searchParams.get('mountpoint') === '/data' && url.searchParams.get('fstype') === 'ext4' && url.searchParams.get('metric') === 'node.filesystem.used_percent')
  await visibleChart(page)
  await page.reload()
  await visibleChart(page)
  await expect(page.getByTestId('resource-selected-mount')).toContainText('/data')
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query).toMatchObject({ device: '/dev/fixture', mountpoint: '/data', fstype: 'ext4', metrics: 'node.filesystem.used_percent' })
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: 'CPU 忙碌率（1 分钟）', exact: true }).click()
  await visibleChart(page)
  expect(new URL(page.url()).searchParams.has('mountpoint')).toBe(false)
})

test('filesystem failure clears its own table while valid scalar cards and trends remain usable', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  await visibleChart(page)
  state.filesystemMode = 'budget'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-filesystem-error')).toContainText('查询超过当前预算')
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('/data')
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('20.00 %')
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await visibleChart(page)
  state.filesystemMode = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('/data')
})


test('incomplete filesystem identity shows an error without querying another resource', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?metric=node.filesystem.used_percent&device=partial`)
  await expect(page.getByTestId('resource-error')).toContainText('资源维度选择无效')
  await expect(page.getByTestId('resource-cores')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
  expect(new URL(page.url()).searchParams.get('metric')).toBe('node.filesystem.used_percent')
})


test('inode missing statistics remain empty while byte capacity is valid, then valid zero restores with exact trend URL', async ({ page }) => {
  const state = await resourceBackend(page)
  state.inodeMode = 'inode-missing'
  await page.goto(`/node-resources/${node}?refresh=off`)
  const table = page.getByTestId('resource-filesystem-table')
  await expect(table).toContainText('73.68 %')
  await expect(table).toContainText('缺失')
  state.inodeMode = 'inode-empty'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(table).toContainText('0.00 %')
  await table.getByRole('button', { name: '查看趋势' }).click()
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: 'inode 使用率', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('metric') === 'node.filesystem.inodes_used_percent' && url.searchParams.get('mountpoint') === '/data')
  await page.reload()
  await expect(page.getByTestId('resource-metric')).toContainText('inode 使用率')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query).toMatchObject({ metrics: 'node.filesystem.inodes_used_percent', device: '/dev/fixture', mountpoint: '/data', fstype: 'ext4' })
})
test('inode budget failure clears only inode values and recovery stays in the same refresh lifecycle', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('20.00 %')
  state.inodeMode = 'budget'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-inode-error')).toContainText('查询超过当前预算')
  await expect(page.getByTestId('resource-filesystem-table')).not.toContainText('20.00 %')
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('73.68 %')
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  state.inodeMode = ''
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('resource-inode-error')).toHaveCount(0)
  await expect(page.getByTestId('resource-filesystem-table')).toContainText('20.00 %')
})

test('reports a collection failure without presenting it as zero load or unsupported filesystem', async ({ page }) => {
  await resourceBackend(page, { mode: 'collection-failed' })
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(page.getByTestId('resource-collection-status')).toContainText('采集失败')
  await expect(page.getByTestId('resource-filesystem-capability')).toHaveCount(0)
})
test('successful restricted source explains uncollected filesystem separately', async ({ page }) => {
  await resourceBackend(page, { mode: 'filesystem-uncollected' })
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(page.getByTestId('resource-filesystem-capability')).toContainText('未提供文件系统指标')
  await expect(page.getByTestId('resource-collection-status')).toHaveCount(0)
})


for (const [family, device, firstMetric, secondMetric, secondLabel, emptyLabel] of [
  ['disk', 'sda', 'node.disk.read_bytes_per_second', 'node.disk.write_bytes_per_second', '磁盘写入吞吐', '暂无磁盘吞吐数据'],
  ['network', 'eth0', 'node.network.receive_bytes_per_second', 'node.network.transmit_bytes_per_second', '网络发送吞吐', '暂无网络吞吐数据']
]) {
  test(`${family} throughput restores a device, isolates missing collection and clears denied data`, async ({ page }) => {
    const state = await resourceBackend(page)
    await page.goto(`/node-resources/${node}?refresh=off`)
    const table = page.getByTestId(`resource-${family}-table`)
    await expect(table).toContainText(device)
    await expect(table).toContainText('2.00 KiB/s')
    await table.getByRole('button', { name: '查看趋势' }).click()
    await expect(page.getByTestId('resource-selected-device')).toHaveText(device)
    await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    const reads = state.reads.filter(item => item.path.endsWith('resource_trends'))
    expect(reads.at(-1).query).toMatchObject({ metrics: firstMetric, device: device })
    expect(reads.at(-1).query.mountpoint).toBeUndefined()
    await page.reload()
    await expect(page.getByTestId('resource-selected-device')).toHaveText(device)
    await page.getByTestId('resource-metric').click()
    await page.getByRole('option', { name: secondLabel, exact: true }).click()
    await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query).toMatchObject({ metrics: secondMetric, device })
    state[`${family}Mode`] = `${family}-missing`
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(table).toContainText(emptyLabel)
    await expect(page.getByTestId('resource-memoryTotal')).toContainText('16.00 GiB')
    state[`${family}Mode`] = 'denied'
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('resource-cores')).toHaveCount(0)
    await expect(table).toHaveCount(0)
  })
}


test('disk IO timing uses percent and ms and restores all three exact device trends', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  const table = page.getByTestId('resource-disk-table')
  await expect(table).toContainText('0.00 %')
  await expect(table).toContainText('2.50 ms')
  await expect(page.getByTestId('resource-disks')).toContainText('包含排队与处理')
  await table.getByRole('button', { name: '查看趋势' }).click()
  for (const [label, key] of [['IO 忙碌时间占比', 'node.disk.io_busy_percent'], ['平均读取耗时', 'node.disk.read_mean_duration_milliseconds'], ['平均写入耗时', 'node.disk.write_mean_duration_milliseconds']]) {
    await page.getByTestId('resource-metric').click()
    await page.getByRole('option', { name: label, exact: true }).click()
    await expect.poll(() => state.reads.filter(item => item.path.endsWith('resource_trends')).at(-1)?.query).toMatchObject({ metrics: key, device: 'sda' })
    await page.reload()
    await expect(page.getByTestId('resource-metric')).toContainText(label)
    await expect(page.getByTestId('resource-selected-device')).toHaveText('sda')
    await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  }
  state.diskMode = 'disk-idle'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(table).toContainText('0.00 %')
  await expect(table.locator('strong').filter({ hasText: '—' })).toHaveCount(2)
  await expect(table).not.toContainText('0.00 ms')
  await expect(page.getByTestId('resource-cores')).toContainText('0 核')
})
