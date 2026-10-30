import { expect, test } from '@playwright/test'
import { readFilesystemMountTable, readResourceHeatmap, readBlockCapacityTable } from './online/node-resource-receipts.js'
import { identity, mount, node, observations, resourceBackend, resourcePermissions, secondNode } from '../../../monitor/frontend/e2e/node-resources.fixture'

const isCapacityRequest = request => {
  const url = new URL(request.url())
  return url.pathname === '/api/v1/monitor/platform/resource_observations' && url.searchParams.get('metrics') === 'node.block.capacity_bytes'
}

test('host detail tips translate in the same Console iframe without querying or hiding current states', async ({ page }) => {
  const state = await resourceBackend(page)
  state.filesystemMode = 'filesystem-uncollected'; state.inodeMode = 'filesystem-uncollected'
  await page.goto(`/monitor/node-resources/${node}?range=5m&refresh=off`)
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('node-resources')).toHaveAttribute('aria-busy', 'false')
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const origin = await monitor.locator('body').evaluate(() => performance.timeOrigin)
  const count = state.reads.length, url = page.url()
  const filesystemHelp = monitor.getByTestId('resource-filesystems').getByRole('heading', { level: 3 }).getByRole('button')
  await filesystemHelp.focus()
  await expect(monitor.getByRole('tooltip')).toContainText('不作物理总量汇总')
  await filesystemHelp.blur()
  await expect(monitor.getByRole('tooltip')).not.toBeVisible()
  const help = monitor.getByTestId('resource-trend-help').getByRole('button')
  await help.focus()
  await expect(monitor.getByRole('tooltip')).toContainText('右轴：内存使用率（%）')
  await expect(monitor.getByRole('tooltip')).toContainText('时段对比的范围保持不变')
  await expect(monitor.getByTestId('resource-filesystem-capability')).toContainText('未提供文件系统指标')
  await expect(monitor.getByTestId('resource-trend-window')).toBeVisible()
  await expect(monitor.getByTestId('resource-paired-values')).toContainText('16.00 GiB')
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await page.setViewportSize({ width: 620, height: 900 })
  await help.focus()
  await expect(monitor.getByRole('tooltip')).toContainText('right axis: memory usage (%)')
  await expect(monitor.getByTestId('resource-filesystem-capability')).toContainText('does not provide filesystem metrics')
  expect(await monitor.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  expect(state.reads).toHaveLength(count)
  expect(page.url()).toBe(url)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

for (const locale of ['zh-cn', 'en']) test(`host automatic filters retain one Console iframe and owner query (${locale})`, async ({ page }) => {
  const state = await resourceBackend(page, { locale })
  const reads = () => state.reads.filter(read => read.path.endsWith('/host_nodes'))
  await page.clock.install()
  await page.goto('/monitor/node-resources?page=2&page_size=10&refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: locale === 'en' ? 'View resources' : '查看资源' })).toHaveCount(2)
  const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin)
  const input = monitor.getByTestId('resource-search')
  await expect(monitor.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })).toHaveCount(0)
  await input.fill('N'); await input.fill('Node')
  expect(reads()).toHaveLength(1)
  await page.clock.fastForward(301)
  await expect(page).toHaveURL(url => url.pathname === '/monitor/node-resources' && url.searchParams.get('search') === 'Node' && !url.searchParams.has('page'))
  await expect.poll(() => reads().length).toBe(2)
  expect(reads().at(-1).query).toEqual({ page: '1', page_size: '10', search: 'Node' })
  await monitor.getByTestId('resource-reset').click()
  await expect.poll(() => reads().length).toBe(3)
  await expect(page).toHaveURL('/monitor/node-resources?page_size=10&refresh=off')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
})

test('node resource navigation preserves the real Monitor iframe and one Console history', async ({ page }) => {
  await resourceBackend(page)
  await page.goto('/monitor/node-resources?page=2')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('主机监控')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '查看资源' }).first().click()
  await expect(page).toHaveURL(new RegExp(`/monitor/node-resources/${node}\\?page=2$`))
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.goBack()
  await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  await page.goForward()
  await expect(monitor.getByTestId('resource-cores')).toContainText('0 核')
  await page.reload()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
})
test('Tenant navigation hides the entry and never mounts the platform resource iframe', async ({ page }) => {
  const state = await resourceBackend(page, { identity: identity(resourcePermissions, { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/node-resources')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(state.reads).toHaveLength(0)
})


test('host list refresh choice stays in Console URL across detail, back and reload', async ({ page }) => {
  await resourceBackend(page)
  await page.goto('/monitor/node-resources?page=2&refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('resource-refresh-status')).toContainText('手工')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '查看资源' }).first().click()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page).toHaveURL(url => url.searchParams.get('refresh') === 'off' && url.pathname.endsWith(node))
  await monitor.getByRole('button', { name: '返回主机列表' }).click()
  await expect(page).toHaveURL('/monitor/node-resources?page=2&refresh=off')
  await expect(monitor.getByTestId('resource-refresh-status')).toContainText('手工')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.reload()
  await expect(monitor.getByTestId('resource-refresh-status')).toContainText('手工')
})

test('host type and management state restore through one Console history and iframe', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto('/monitor/node-resources?node_kind=virtual&enabled=false&search=Node&page=2&refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('button', { name: '查看资源' })).toHaveCount(2)
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await monitor.getByRole('button', { name: '查看资源' }).first().click()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page).toHaveURL(url => url.pathname.endsWith(node) && url.searchParams.get('node_kind') === 'virtual' && url.searchParams.get('enabled') === 'false')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.goBack()
  await expect(monitor.locator('.host-node-filters')).toContainText('虚拟机')
  await expect(monitor.locator('.host-node-filters')).toContainText('停用')
  await page.goForward()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  await page.reload()
  await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  await monitor.getByRole('button', { name: '返回主机列表', exact: true }).click()
  await expect(monitor.getByTestId('resource-search')).toHaveValue('Node')
  expect(state.reads.filter(read => read.path.endsWith('/host_nodes')).at(-1).query).toEqual({ page: '2', page_size: '20', search: 'Node', node_kind: 'virtual', enabled: 'false' })
})

test('filesystem list open, changed-value refresh and collapse preserve the Console iframe and exact receipts', async ({ page }) => {
  let changed = false
  const state = await resourceBackend(page, { observationResponse: body => {
    if (!changed || !body.series[0].metric_key.startsWith('node.filesystem.inodes_')) return
    const values = { inodes_total: 100, inodes_free: 70, inodes_used: 30, inodes_used_percent: 30 }
    for (const series of body.series) for (const point of series.points) point.value = values[series.metric_key.split('.').at(-1)]
  } })
  const resources = [], pending = []
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/monitor/platform/resource_observations') {
      pending.push(response.json().then(value => resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
    }
  })
  await page.goto('/monitor/node-resources?refresh=off')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  const button = monitor.getByTestId(`resource-list-filesystem-${node}`)
  const panel = monitor.getByTestId('resource-detail-expansion')
  const table = panel.getByTestId('resource-filesystem-table')
  await expect(button).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
  expect(resources).toHaveLength(0)
  await button.click()
  await expect(button).toHaveAttribute('aria-expanded', 'true')
  await expect(table).toBeVisible()
  await expect(panel.getByTestId('resource-detail-refresh')).toBeEnabled()
  await Promise.all(pending)
  const { receipt: opened } = await readFilesystemMountTable(table, resources, node, expect)
  expect(resources).toHaveLength(2)
  expect([opened.capacity_index, opened.inode_index].sort()).toEqual([0, 1])
  expect(opened.metrics['node.filesystem.inodes_used_percent']).toBe('20.00 %')
  changed = true
  await panel.getByTestId('resource-detail-refresh').click()
  await expect.poll(async () => { await Promise.all(pending); return resources.length }).toBe(4)
  await expect(panel.getByTestId('resource-detail-refresh')).toBeEnabled()
  const { receipt: refreshed } = await readFilesystemMountTable(table, resources, node, expect)
  expect([refreshed.capacity_index, refreshed.inode_index].sort()).toEqual([2, 3])
  expect(refreshed.metrics['node.filesystem.inodes_used_percent']).toBe('30.00 %')
  expect(refreshed.metrics['node.filesystem.inodes_free']).toBe('70 个')
  expect(page.url()).toBe(url)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  await button.click()
  await expect(button).toHaveAttribute('aria-expanded', 'false')
  await expect(panel).toHaveCount(0)
  await Promise.all(pending)
  expect(resources).toHaveLength(4)
  expect(state.reads.filter(row => row.path.endsWith('/resource_observations')).every(row => row.query.node_id === node)).toBe(true)
  expect(page.url()).toBe(url)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
})

test('heatmap DOM receipt keeps zero/full-scale, mount identities and neutral stale or missing values in the Console iframe', async ({ page }) => {
  let stateName = 'valid'
  const state = await resourceBackend(page, { mounts: [mount, { ...mount, device: '/dev/other', fstype: 'xfs' }], observationResponse: body => {
    for (const line of body.series) if (['node.cpu.busy_percent', 'node.memory.used_percent'].includes(line.metric_key) || line.metric_key.startsWith('node.filesystem.')) {
      Object.assign(line.points[0], { data_state: stateName, value: stateName === 'no_data' ? null
        : line.metric_key.startsWith('node.filesystem.') ? line.points[0].value : line.metric_key === 'node.cpu.busy_percent' ? 0 : 100,
        sampled_at: stateName === 'no_data' ? null : line.points[0].sampled_at })
    }
  } })
  const resources = [], pending = []
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/monitor/platform/resource_observations') pending.push(response.json().then(value =>
      resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
  })
  await page.goto(`/monitor/node-resources/${node}?refresh=off`)
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  const panel = monitor.getByTestId('resource-heatmap')
  for (const next of ['valid', 'stale', 'no_data']) {
    if (next !== 'valid') { stateName = next; await monitor.getByRole('button', { name: '刷新', exact: true }).click() }
    await expect(monitor.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
    await Promise.all(pending)
    const before = resources.length, sourceReads = state.reads.length
    const value = await readResourceHeatmap(panel, resources, node, expect)
    expect(value.partitions).toHaveLength(2)
    expect(value.partitions[0].dimensions.mountpoint).toBe(value.partitions[1].dimensions.mountpoint)
    expect(value.partitions[0].dimensions.device).not.toBe(value.partitions[1].dimensions.device)
    expect(value.scalars['node.cpu.busy_percent']).toMatchObject({ data_state: next, value: next === 'valid' ? 0 : null, neutral: next !== 'valid' })
    expect(value.scalars['node.memory.used_percent'].text).toBe(next === 'valid' ? '100.00 %' : '—')
    await expect(monitor.getByTestId('resource-cpu-idle')).toHaveText(next === 'valid' ? 'CPU 空闲率：100.00 %' : 'CPU 空闲率：—')
    expect(resources).toHaveLength(before)
    expect(state.reads).toHaveLength(sourceReads)
  }
})


test('capacity DOM receipt binds all actual device cells and new refresh to the same Console iframe', async ({ page }) => {
  let bytes = 2 ** 40
  await resourceBackend(page, { observationResponse(body, query) {
    if (query.get('metrics') !== 'node.block.capacity_bytes') return
    body.collection.block_capacity = 'available'
    body.series = ['device', 'mapped'].map((kind, index) => ({ ...body.series[0], dimensions: { device: index ? 'dm-0' : 'vda', kind }, points: body.series[0].points.map(point => ({ ...point, value: bytes })) }))
  } })
  const resources = [], pending = []
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/monitor/platform/resource_observations') pending.push(response.json().then(value => resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
  })
  await page.goto(`/monitor/node-resources/${node}?refresh=off`)
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('resource-memoryTotal')).toBeVisible()
  const iframe = page.frames().find(frame => frame.parentFrame()), origin = await iframe.evaluate(() => performance.timeOrigin)
  const url = page.url()
  await monitor.getByTestId('resource-block-toggle').click()
  const panel = monitor.getByTestId('block-capacity-section'), table = panel.getByTestId('resource-block-table')
  await expect(table).toContainText('1.00 TiB')
  await Promise.all(pending)
  const opened = await readBlockCapacityTable(table, resources, node, expect)
  expect(opened.devices.map(row => row.bytes)).toEqual([2 ** 40, 2 ** 40])
  bytes = 2 ** 39
  await panel.getByTestId('resource-detail-refresh').click()
  await expect(table).toContainText('512.00 GiB')
  await Promise.all(pending)
  const refreshed = await readBlockCapacityTable(table, resources, node, expect)
  expect(refreshed.resource_index).toBe(opened.resource_index + 1)
  expect(refreshed.devices.map(row => row.bytes)).toEqual([2 ** 39, 2 ** 39])
  await monitor.getByTestId('resource-block-toggle').click()
  await expect(table).toHaveCount(0)
  expect(page.url()).toBe(url)
  expect(await iframe.evaluate(() => performance.timeOrigin)).toBe(origin)
})

for (const entry of ['list', 'detail']) test(`capacity disabled, source/query failure and recovery clear old values in the Console ${entry} iframe`, async ({ page }) => {
  let capacityState = 'available', bytes = 2 ** 40, generation = 0
  const state = await resourceBackend(page, { observationResponse(body, query) {
    if (query.get('metrics') !== 'node.block.capacity_bytes') return
    body.collection.block_capacity = ['http-error', 'invalid-frame'].includes(capacityState) ? 'available' : capacityState
    const template = body.series[0]
    body.series = ['available', 'stale', 'invalid-frame'].includes(capacityState)
      ? ['device', 'mapped'].map((kind, index) => ({ ...template,
        dimensions: { device: index ? `dm-${generation}` : `disk-${generation}`, kind: capacityState === 'invalid-frame' ? 'physical' : kind },
        points: template.points.map(point => ({ ...point, value: bytes, data_state: capacityState === 'stale' ? 'stale' : 'valid',
          sampled_at: capacityState === 'stale' ? new Date(Date.parse(point.evaluated_at) - 61000).toISOString() : point.evaluated_at })) }))
      : [{ ...template, dimensions: {}, points: template.points.map(point => ({ ...point, value: null, sampled_at: null, data_state: 'no_data' })) }]
  } })
  let releaseFailure
  const failureGate = new Promise(resolve => { releaseFailure = resolve })
  await page.route('**/api/v1/monitor/platform/resource_observations?**', async route => {
    const query = new URL(route.request().url()).searchParams
    if (capacityState !== 'http-error' || query.get('metrics') !== 'node.block.capacity_bytes') return route.fallback()
    await failureGate
    await route.fulfill({ status: 503, json: { error_code: 'observability_backend_unavailable', error: 'fixture unavailable' } })
  })
  const resources = [], pending = [], requests = []
  page.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname.startsWith('/api/v1/monitor/platform/resource_')) requests.push({ path: url.pathname, method: request.method(), query: Object.fromEntries(url.searchParams) })
  })
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/monitor/platform/resource_observations' && url.searchParams.get('metrics') === 'node.block.capacity_bytes') {
      pending.push(response.json().then(value => resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })))
    }
  })
  await page.goto(entry === 'list' ? '/monitor/node-resources?refresh=off' : `/monitor/node-resources/${node}?refresh=off`)
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
  const panel = monitor.getByTestId(entry === 'list' ? 'resource-detail-expansion' : 'block-capacity-section')
  const table = panel.getByTestId('resource-block-table'), refresh = panel.getByTestId('resource-detail-refresh')
  await expect(toggle).toBeVisible()
  if (entry === 'detail') await expect(monitor.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const healthyValue = entry === 'list' ? monitor.getByTestId('resource-summary-cores').first()
    : monitor.getByTestId('resource-cores').locator('strong')
  await expect(healthyValue).toHaveText('0 核')
  const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
  const nonCapacityReads = () => requests.filter(row => row.query.metrics !== 'node.block.capacity_bytes').length
  const unrelatedReads = nonCapacityReads()
  expect(resources).toHaveLength(0)
  await toggle.click()
  await expect(table).toContainText('1.00 TiB')
  await expect.poll(() => resources.length).toBe(1)
  const opened = await readBlockCapacityTable(table, resources, node, expect)
  expect(opened.devices.map(row => row.bytes)).toEqual([bytes, bytes])

  for (const failure of [
    { state: 'not_collected', text: '未启用设备容量采集' },
    { state: 'failed', text: '设备容量采集失败或证据不完整' },
    { state: 'stale', text: '设备容量采样已过期' },
    { state: 'http-error', text: '无法读取资源观测，请稍后刷新。', error: true },
    { state: 'invalid-frame', text: '资源响应不完整，无法展示，请重新刷新。', error: true }
  ]) {
    capacityState = failure.state
    const before = resources.length
    const previousDevices = (await readBlockCapacityTable(table, resources, node, expect)).devices
    const previousRows = await table.locator('.el-table__body tr').allTextContents()
    await refresh.click()
    if (failure.state === 'http-error') {
      // Same-context refresh retains accepted rows while pending; the failed response must then clear them.
      try {
        await expect.poll(() => requests.filter(row => row.query.metrics === 'node.block.capacity_bytes').length).toBe(before + 1)
        await expect(table.locator('.el-table__body tr')).toHaveText(previousRows)
        await expect(refresh).toBeDisabled()
      } finally { releaseFailure() }
    }
    await expect.poll(() => resources.length).toBe(before + 1)
    await expect(refresh).toBeEnabled()
    await expect(panel.getByTestId(failure.error ? 'resource-block-error' : 'resource-block-status')).toHaveText(failure.text)
    await expect(panel.getByTestId(failure.error ? 'resource-block-status' : 'resource-block-error')).toHaveCount(0)
    await expect(table.locator('.el-table__body tr')).toHaveCount(0)
    await expect(table).toContainText('暂无有效设备容量')
    for (const row of previousDevices) await expect(table).not.toContainText(row.dimensions.device)
    await expect(table).not.toContainText('0.00 B')
    await expect(healthyValue).toHaveText('0 核')
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    await expect(monitor.getByTestId('resource-summary-error')).toHaveCount(0)

    capacityState = 'available'; bytes /= 2; generation += 1
    await refresh.click()
    await expect.poll(() => resources.length).toBe(before + 2)
    await expect(refresh).toBeEnabled()
    const recovered = await readBlockCapacityTable(table, resources, node, expect)
    expect(recovered.resource_index).toBe(before + 1)
    expect(recovered.devices.map(row => row.bytes)).toEqual([bytes, bytes])
    expect(recovered.devices.map(row => row.dimensions.device)).toEqual([`disk-${generation}`, `dm-${generation}`])
    for (const row of previousDevices) await expect(table).not.toContainText(row.dimensions.device)
    await expect(panel.getByTestId('resource-block-status')).toHaveCount(0)
    await expect(panel.getByTestId('resource-block-error')).toHaveCount(0)
    expect(page.url()).toBe(url)
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
    expect(nonCapacityReads()).toBe(unrelatedReads)
  }
  await toggle.click()
  await expect(panel).toHaveCount(entry === 'list' ? 0 : 1)
  await expect(table).toHaveCount(0)
  await Promise.all(pending)
  expect(resources).toHaveLength(11)
  expect(requests.filter(row => row.query.metrics === 'node.block.capacity_bytes')).toHaveLength(11)
  expect(requests.filter(row => row.query.metrics === 'node.block.capacity_bytes').every(row => row.method === 'GET'
    && row.path === '/api/v1/monitor/platform/resource_observations'
    && JSON.stringify(Object.keys(row.query).sort()) === JSON.stringify(['metrics', 'node_id']) && row.query.node_id === node)).toBe(true)
  expect(state.reads.every(row => !row.method || row.method === 'GET')).toBe(true)
  expect(page.url()).toBe(url)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
})

for (const entry of ['list', 'detail']) test(`capacity automatic refresh pauses, cancels in-flight reads and resumes in the Console ${entry} iframe`, async ({ page }) => {
  await page.clock.install()
  let bytes = 2 ** 40
  const capacityBody = body => {
    body.collection.block_capacity = 'available'
    body.series[0].dimensions = { device: 'vda', kind: 'device' }
    body.series[0].points[0].value = bytes
  }
  const state = await resourceBackend(page, { observationResponse(body, query) {
    if (query.get('metrics') === 'node.block.capacity_bytes') capacityBody(body)
  } })
  const requests = [], resources = [], pending = []
  page.on('request', request => { if (isCapacityRequest(request)) requests.push(request) })
  page.on('response', response => {
    if (isCapacityRequest(response.request())) pending.push(response.json().then(value => {
      const url = new URL(response.url())
      resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })
    }))
  })
  let holdNext = false, releaseHeld, heldRequest, finishHeld
  const held = new Promise(resolve => { releaseHeld = resolve })
  const heldFinished = new Promise(resolve => { finishHeld = resolve })
  await page.route('**/api/v1/monitor/platform/resource_observations?**', async route => {
    if (!holdNext || !isCapacityRequest(route.request())) return route.fallback()
    holdNext = false; heldRequest = route.request()
    const body = observations(node, ['node.block.capacity_bytes'], false, '', state.serverEnd)
    capacityBody(body)
    await held
    try { await route.fulfill({ json: body }) } finally { finishHeld() }
  })
  try {
    await page.goto(entry === 'list' ? '/monitor/node-resources?refresh=10' : `/monitor/node-resources/${node}?refresh=10`)
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
    const panel = monitor.getByTestId(entry === 'list' ? 'resource-detail-expansion' : 'block-capacity-section')
    const table = panel.getByTestId('resource-block-table'), refresh = panel.getByTestId('resource-detail-refresh')
    const pageRefresh = monitor.getByRole('button', { name: '刷新', exact: true })
    await expect(toggle).toBeVisible()
    await expect(pageRefresh).toBeEnabled()
    const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
    const visibility = async hidden => frame.evaluate(value => {
      Object.defineProperty(document, 'hidden', { configurable: true, value })
      document.dispatchEvent(new Event('visibilitychange'))
    }, hidden)
    const monitorReads = () => state.reads.filter(row => row.path.startsWith('/api/v1/monitor/platform/resource_')).length
    const readCapacity = async (count, value) => {
      await expect.poll(() => requests.length).toBe(count)
      await expect(refresh).toBeEnabled()
      await expect(table).toContainText('vda')
      await Promise.all(pending)
      const reading = await readBlockCapacityTable(table, resources, node, expect)
      expect(reading.devices.map(row => row.bytes)).toEqual([value])
      expect(resources.at(-1).value.end).toBe(state.serverEnd)
      expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
      return reading
    }
    expect(requests).toHaveLength(0)
    await toggle.click()
    const opened = await readCapacity(1, bytes)
    bytes = 2 ** 39; state.serverEnd = '2026-10-07T00:00:10Z'
    await page.clock.fastForward(10001)
    const automatic = await readCapacity(2, bytes)
    expect(automatic.resource_index).toBe(opened.resource_index + 1)
    await expect(pageRefresh).toBeEnabled()

    await visibility(true)
    await expect(monitor.getByTestId('resource-refresh-status')).toHaveText('页面隐藏，自动刷新已暂停')
    const hiddenReads = monitorReads()
    await page.clock.fastForward(60001)
    expect(requests).toHaveLength(2)
    expect(monitorReads()).toBe(hiddenReads)
    bytes = 2 ** 38; state.serverEnd = '2026-10-07T00:00:20Z'
    await visibility(false)
    const resumed = await readCapacity(3, bytes)
    const resumedRows = await table.locator('.el-table__body tr').allTextContents()
    expect(resumed.resource_index).toBe(automatic.resource_index + 1)
    await expect(monitor.getByTestId('resource-refresh-status')).toHaveText('自动刷新已开启')

    // A slow automatic read must not overlap or survive a visibility pause.
    bytes = 2 ** 43; state.serverEnd = '2026-10-07T00:00:30Z'; holdNext = true
    await page.clock.fastForward(10001)
    await expect.poll(() => Boolean(heldRequest)).toBe(true)
    expect(requests).toHaveLength(4)
    await expect(refresh).toBeDisabled()
    await expect(table.locator('.el-table__body tr')).toHaveText(resumedRows)
    await page.clock.fastForward(60001)
    expect(requests).toHaveLength(4)
    const cancelled = page.waitForEvent('requestfailed', request => request === heldRequest)
    await visibility(true)
    const failed = await cancelled
    expect(failed.failure()).not.toBeNull()
    releaseHeld(); await heldFinished
    const pausedReads = monitorReads()
    await page.clock.fastForward(60001)
    expect(requests).toHaveLength(4)
    expect(monitorReads()).toBe(pausedReads)
    await expect(table).not.toContainText('8.00 TiB')

    bytes = 2 ** 37; state.serverEnd = '2026-10-07T00:00:40Z'
    await visibility(false)
    const recovered = await readCapacity(5, bytes)
    expect(recovered.resource_index).toBe(resumed.resource_index + 1)
    bytes = 2 ** 36; state.serverEnd = '2026-10-07T00:00:50Z'
    await page.clock.fastForward(10001)
    const nextAutomatic = await readCapacity(6, bytes)
    expect(nextAutomatic.resource_index).toBe(recovered.resource_index + 1)
    await expect(table).not.toContainText('8.00 TiB')
    expect(page.url()).toBe(url)

    // A refresh preference change retains the same data context and temporary expansion.
    bytes = 2 ** 35; state.serverEnd = '2026-10-07T00:01:00Z'
    await monitor.getByTestId('resource-refresh').click()
    await monitor.getByRole('option', { name: '关闭自动刷新', exact: true }).click()
    await expect(page).toHaveURL(current => current.searchParams.get('refresh') === 'off')
    await expect(pageRefresh).toBeEnabled()
    if (entry === 'list') { await expect(table).toBeVisible(); await expect(toggle).toHaveAttribute('aria-expanded', 'true') }
    await readCapacity(7, bytes)
    await expect(monitor.getByTestId('resource-refresh-status')).toHaveText('手工刷新')
    await page.clock.fastForward(60001)
    await visibility(true); await visibility(false)
    await page.clock.fastForward(60001)
    expect(requests).toHaveLength(7)
    bytes = 2 ** 34; state.serverEnd = '2026-10-07T00:01:10Z'
    await refresh.click()
    await readCapacity(8, bytes)
    await toggle.click()
    await expect(table).toHaveCount(0)
    await page.clock.fastForward(60001)
    expect(requests).toHaveLength(8)
    expect(requests.every(request => request.method() === 'GET'
      && new URL(request.url()).searchParams.get('node_id') === node
      && [...new URL(request.url()).searchParams.keys()].sort().join(',') === 'metrics,node_id')).toBe(true)
    expect(new URL(page.url()).searchParams.get('refresh')).toBe('off')
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  } finally { releaseHeld() }
})

// Hold one immutable old frame at the HTTP boundary; later requests use the existing fixture.
async function heldCapacityFixture(page, lateStatus = 200, holdAt = 1) {
  await page.clock.install()
  const state = await resourceBackend(page, { observationResponse(body, query) {
    if (query.get('metrics') !== 'node.block.capacity_bytes') return
    const first = query.get('node_id') === node
    body.collection.block_capacity = 'available'
    body.series[0].dimensions = { device: first ? 'current-a' : 'current-b', kind: 'device' }
    body.series[0].points[0].value = 2 ** (first ? 35 : 36)
  } })
  const requests = [], resources = [], pending = []
  page.on('request', request => { if (isCapacityRequest(request)) requests.push(request) })
  page.on('response', response => {
    if (isCapacityRequest(response.request())) pending.push(response.json().then(value => {
      const url = new URL(response.url())
      resources.push({ path: url.pathname, query: Object.fromEntries(url.searchParams), value })
    }))
  })
  let heldRequest, release, finished, capacityCalls = 0
  const gate = new Promise(resolve => { release = resolve }), settled = new Promise(resolve => { finished = resolve })
  await page.route('**/api/v1/monitor/platform/resource_observations?**', async route => {
    if (!isCapacityRequest(route.request())) return route.fallback()
    capacityCalls += 1
    if (capacityCalls !== holdAt) return route.fallback()
    heldRequest = route.request()
    const body = observations(node, ['node.block.capacity_bytes'])
    body.collection.block_capacity = 'available'
    body.series[0].dimensions = { device: 'discarded-capacity', kind: 'mapped' }
    body.series[0].points[0].value = 2 ** 43
    await gate
    try {
      await route.fulfill({ status: lateStatus, json: lateStatus === 200 ? body : { error_code: 'permission_denied', error: 'fixture denied' } })
    } finally { finished() }
  })
  return {
    state, requests, resources, release,
    waitHeld: () => expect.poll(() => Boolean(heldRequest)).toBe(true),
    async cancelThrough(action) {
      const cancelled = page.waitForEvent('requestfailed', request => request === heldRequest)
      await action()
      expect((await cancelled).failure()).not.toBeNull()
    },
    async releaseOld() { release(); await settled; await Promise.all(pending) },
    async read(table, id) {
      await expect(table).toContainText(id === node ? 'current-a' : 'current-b')
      await Promise.all(pending)
      const value = await readBlockCapacityTable(table, resources, id, expect)
      expect(value.devices).toHaveLength(1)
      expect(value.devices[0].bytes).toBe(2 ** (id === node ? 35 : 36))
      await expect(table).not.toContainText('discarded-capacity')
      await expect(table).not.toContainText('8.00 TiB')
      return value
    }
  }
}

for (const lateStatus of [200, 403]) test(`Console capacity host switch cancels an old ${lateStatus} response without poisoning the new host`, async ({ page }) => {
  const source = await heldCapacityFixture(page, lateStatus)
  try {
    await page.goto('/monitor/node-resources?page=2&refresh=off')
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const first = monitor.getByTestId(`resource-list-block-${node}`), second = monitor.getByTestId(`resource-list-block-${secondNode}`)
    await expect(first).toBeVisible()
    const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
    await first.click(); await source.waitHeld()
    await source.cancelThrough(() => second.click())
    const panel = monitor.getByTestId('resource-detail-expansion'), table = panel.getByTestId('resource-block-table')
    await expect(panel).toHaveCount(1)
    await expect(first).toHaveAttribute('aria-expanded', 'false')
    await expect(second).toHaveAttribute('aria-expanded', 'true')
    const current = await source.read(table, secondNode)
    await source.releaseOld()
    const after = await source.read(table, secondNode)
    expect(after.resource_index).toBe(current.resource_index)
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    await expect(panel.getByTestId('resource-block-error')).toHaveCount(0)
    await page.clock.fastForward(60001)
    expect(source.requests.map(request => new URL(request.url()).searchParams.get('node_id'))).toEqual([node, secondNode])
    expect(source.resources).toHaveLength(1)
    expect(page.url()).toBe(url)
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  } finally { source.release() }
})

for (const entry of ['list', 'detail']) test(`Console ${entry} capacity collapse cancels a delayed denial and reopening starts a clean view`, async ({ page }) => {
  const source = await heldCapacityFixture(page, 403)
  try {
    await page.goto(entry === 'list' ? '/monitor/node-resources?page=2&refresh=off' : `/monitor/node-resources/${node}?page=2&refresh=off`)
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
    const panel = monitor.getByTestId(entry === 'list' ? 'resource-detail-expansion' : 'block-capacity-section')
    const table = panel.getByTestId('resource-block-table')
    await expect(toggle).toBeVisible()
    const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
    await toggle.click(); await source.waitHeld()
    await source.cancelThrough(() => toggle.click())
    await expect(table).toHaveCount(0)
    expect(source.requests).toHaveLength(1)
    await toggle.click()
    const current = await source.read(table, node)
    await source.releaseOld()
    const after = await source.read(table, node)
    expect(after.resource_index).toBe(current.resource_index)
    await expect(panel.getByTestId('resource-block-error')).toHaveCount(0)
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    await page.clock.fastForward(60001)
    expect(source.requests).toHaveLength(2)
    expect(source.resources).toHaveLength(1)
    expect(page.url()).toBe(url)
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  } finally { source.release() }
})

for (const entry of ['list', 'detail']) test(`leaving Console ${entry} capacity unloads its pending owner and history return creates a fresh iframe`, async ({ page }) => {
  const source = await heldCapacityFixture(page, 403)
  try {
    await page.goto(entry === 'list' ? '/monitor/node-resources?page=2&refresh=10' : `/monitor/node-resources/${node}?page=2&refresh=10`)
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
    const table = monitor.getByTestId('resource-block-table')
    await expect(toggle).toBeVisible()
    const frame = page.frames().find(frame => frame.parentFrame()), url = page.url()
    await toggle.click(); await source.waitHeld()
    await source.cancelThrough(() => page.locator('.logo-area').click())
    await expect(page).toHaveURL('/')
    await expect(page.locator('.home-view')).toBeVisible()
    await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(0)
    expect(frame.isDetached()).toBe(true)
    await source.releaseOld()
    await page.clock.fastForward(60001)
    expect(source.requests).toHaveLength(1)
    expect(page.url()).toMatch(/\/$/)
    await page.goBack()
    await expect(page).toHaveURL(url)
    await expect(toggle).toBeVisible()
    const next = page.frames().find(item => item.parentFrame())
    expect(next).not.toBe(frame)
    await expect(table).toHaveCount(0)
    await toggle.click()
    await source.read(table, node)
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    expect(source.requests).toHaveLength(2)
    expect(source.resources).toHaveLength(1)
    expect(new URL(page.url()).searchParams.get('refresh')).toBe('10')
  } finally { source.release() }
})

for (const entry of ['list', 'detail']) test(`current capacity denial clears Console ${entry} observations and stops polling until explicit reload`, async ({ page }) => {
  const source = await heldCapacityFixture(page, 403, 2)
  try {
    await page.goto(entry === 'list' ? '/monitor/node-resources?refresh=10' : `/monitor/node-resources/${node}?refresh=10`)
    const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
    const table = monitor.getByTestId('resource-block-table')
    await expect(toggle).toBeVisible()
    const frame = page.frames().find(item => item.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin), url = page.url()
    await toggle.click()
    const initial = await source.read(table, node)
    await page.clock.fastForward(10001)
    await source.waitHeld()
    await source.releaseOld()
    await expect(monitor.getByTestId('resource-error')).toHaveText('当前身份无权查看主机监控。')
    await expect(table).toHaveCount(0)
    await expect(monitor.getByTestId(entry === 'list' ? 'resource-summary-cores' : 'resource-cores')).toHaveCount(0)
    await expect(monitor.getByTestId('resource-refresh-status')).toHaveText('自动刷新已停止，请重新加载')
    const reads = source.state.reads.length
    await page.clock.fastForward(60001)
    expect(source.requests).toHaveLength(2)
    expect(source.state.reads).toHaveLength(reads)
    await monitor.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(toggle).toBeVisible()
    await expect(table).toHaveCount(0)
    await toggle.click()
    const recovered = await source.read(table, node)
    expect(recovered.resource_index).toBeGreaterThan(initial.resource_index)
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    await expect(monitor.getByTestId('resource-refresh-status')).toHaveText('自动刷新已开启')
    expect(source.requests).toHaveLength(3)
    expect(page.url()).toBe(url)
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  } finally { source.release() }
})

async function capacityAuthorityFixture(page) {
  const source = await heldCapacityFixture(page, 200, 2)
  let generation = 0, gate = null, release, waiting = 0
  await page.route('**/api/v1/system/refresh', route => route.fulfill({ json: { access_token: `capacity-authority-${++generation}`, expires_in: 3600 } }))
  await page.route('**/api/v1/system/auth/context', async route => {
    if (gate) { waiting += 1; await gate }
    await route.fulfill({ json: source.state.identity })
  })
  return { ...source,
    async begin(value) {
      source.state.identity = value
      waiting = 0; gate = new Promise(resolve => { release = resolve })
      // Use the same real refresh action as the Console authorization acceptance.
      await page.evaluate(async () => {
        const { useAuthStore } = await import('/src/store/auth.js')
        await useAuthStore().refreshAccessToken({ force: true })
      })
      await expect.poll(() => waiting).toBeGreaterThan(0)
    },
    finish() { gate = null; release?.() }
  }
}

for (const entry of ['list', 'detail']) for (const change of ['permission', 'principal', 'tenant']) test(`Console ${entry} capacity cannot cross a confirmed ${change} authority change`, async ({ page }) => {
  const source = await capacityAuthorityFixture(page)
  try {
    await page.goto(entry === 'list' ? '/monitor/node-resources?page=2&refresh=10' : `/monitor/node-resources/${node}?page=2&refresh=10`)
    const iframe = page.locator('iframe[data-testid="module-iframe"]'), monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
    const toggle = monitor.getByTestId(entry === 'list' ? `resource-list-block-${node}` : 'resource-block-toggle')
    const table = monitor.getByTestId('resource-block-table')
    await expect(toggle).toBeVisible()
    const frame = page.frames().find(item => item.parentFrame()), url = page.url()
    await toggle.click()
    await source.read(table, node)
    await page.clock.fastForward(10001)
    await source.waitHeld()
    const next = identity(change === 'permission' ? ['platform.host_node.read'] : resourcePermissions,
      change === 'tenant' ? { type: 'tenant', tenant_id: '7', tenant_membership_id: '9' } : { type: 'platform' })
    if (change === 'principal') next.principal.id = 'another-platform-user'
    await source.cancelThrough(() => source.begin(next))
    await expect(iframe).toHaveCount(1)
    await expect(iframe).toBeHidden()
    expect(frame.isDetached()).toBe(false)
    await expect(table).toHaveCount(0)
    source.finish()
    if (change !== 'principal') {
      await expect(page.getByText('无权访问此页面', { exact: true })).toBeVisible()
      await expect(iframe).toHaveCount(0)
    } else {
      await expect(iframe).toBeVisible()
      await expect(toggle).toBeVisible()
      await expect(table).toHaveCount(0)
    }
    expect(frame.isDetached()).toBe(true)
    await source.releaseOld()
    await page.clock.fastForward(60001)
    expect(source.requests).toHaveLength(2)
    if (change !== 'principal') {
      // A later authoritative grant starts a new view; it cannot restore the revoked one.
      await source.begin(identity(resourcePermissions))
      await expect(iframe).toHaveCount(0)
      source.finish()
      await expect(iframe).toBeVisible()
      await expect(toggle).toBeVisible()
    }
    const currentFrame = page.frames().find(item => item.parentFrame())
    expect(currentFrame).not.toBe(frame)
    await expect(table).toHaveCount(0)
    const current = await page.evaluate(async () => {
      const { useAuthStore } = await import('/src/store/auth.js')
      const store = useAuthStore()
      return { principal: store.authContext.principal.id, context: store.authContext.context.type, permissions: store.permissions }
    })
    expect(current.principal).toBe(change === 'principal' ? 'another-platform-user' : 'fixture-user')
    expect(current.context).toBe('platform')
    expect(current.permissions).toEqual(expect.arrayContaining(resourcePermissions))
    await toggle.click()
    const fresh = await source.read(table, node)
    expect(fresh.resource_index).toBe(1)
    expect(source.requests).toHaveLength(3)
    expect(source.resources).toHaveLength(2)
    expect(source.requests[2].headers().authorization).not.toBe(source.requests[0].headers().authorization)
    await expect(monitor.getByTestId('resource-error')).toHaveCount(0)
    expect(page.url()).toBe(url)
  } finally { source.finish(); source.release() }
})
