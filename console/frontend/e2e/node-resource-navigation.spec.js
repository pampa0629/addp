import { expect, test } from '@playwright/test'
import { readFilesystemMountTable, readResourceHeatmap } from './online/node-resource-receipts.js'
import { identity, mount, node, resourceBackend, resourcePermissions } from '../../../monitor/frontend/e2e/node-resources.fixture'

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
