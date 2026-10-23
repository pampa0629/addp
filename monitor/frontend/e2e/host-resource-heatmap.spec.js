import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { identity, mount, node, resourceBackend, setIdentity } from './node-resources.fixture'

const cpu = 'node.cpu.busy_percent', memory = 'node.memory.used_percent', partition = 'node.filesystem.used_percent'
const refresh = (page, locale = 'zh-cn') => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
const heatmap = page => page.getByTestId('resource-heatmap')

for (const locale of ['zh-cn', 'en']) test(`host heatmap preserves values, mount identities, themes and trend navigation (${locale})`, async ({ page }) => {
  const values = { [cpu]: 13.37, [memory]: 81.25, [partition]: 73.6842105263158 }
  const state = await resourceBackend(page, { locale, mounts: [mount, { device: '/dev/other', mountpoint: mount.mountpoint, fstype: 'xfs' }], observationResponse: body => {
    for (const line of body.series) if (line.metric_key in values) line.points[0].value = values[line.metric_key]
  } })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/node-resources/${node}?range=5m&refresh=off`)
  const panel = heatmap(page), cpuTile = panel.getByTestId('heatmap-cpuBusy'), memoryTile = panel.getByTestId('heatmap-memoryUsed'), mounts = panel.getByTestId('heatmap-partition')
  await expect(cpuTile.locator('strong')).toHaveText('13.37 %')
  await expect(memoryTile.locator('strong')).toHaveText('81.25 %')
  await expect(mounts).toHaveCount(2)
  for (const tile of [cpuTile, memoryTile, mounts.first(), mounts.last()]) {
    await expect(tile.getByRole('progressbar')).toHaveAttribute('aria-valuemin', '0')
    await expect(tile.getByRole('progressbar')).toHaveAttribute('aria-valuemax', '100')
    await expect(tile).toContainText(locale === 'en' ? 'Sampled at' : '采样时间')
  }
  await expect(cpuTile.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '13.37')
  await expect(mounts.first().locator('strong')).toHaveText('73.68 %')
  await expect(mounts.first()).toContainText('/dev/fixture · ext4')
  await expect(mounts.last()).toContainText('/dev/other · xfs')
  expect(state.reads.filter(read => read.path.endsWith('/resource_observations'))).toHaveLength(5)
  const shots = mkdtempSync(join(tmpdir(), 'addp-heatmap-'))
  await panel.screenshot({ path: join(shots, `heatmap-${locale}-light.png`), animations: 'disabled' })
  const firstColor = await cpuTile.evaluate(element => getComputedStyle(element).borderTopColor)
  await page.evaluate(() => {
    document.documentElement.classList.add('dark')
    document.documentElement.style.setProperty('--el-color-success', 'rgb(20, 120, 80)')
    document.documentElement.style.setProperty('--el-color-danger', 'rgb(200, 20, 90)')
  })
  expect(await cpuTile.evaluate(element => getComputedStyle(element).borderTopColor)).not.toBe(firstColor)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const before = state.reads.length
  await page.evaluate(() => {
    document.documentElement.style.removeProperty('--el-color-success')
    document.documentElement.style.removeProperty('--el-color-danger')
  })
  await panel.screenshot({ path: join(shots, `heatmap-${locale}-dark.png`), animations: 'disabled' })
  console.log(`Host heatmap screenshots: ${shots}`)
  expect(state.reads).toHaveLength(before)
  await mounts.last().getByRole('button').click()
  await expect(page).toHaveURL(url => url.searchParams.get('device') === '/dev/other')
  await expect(page).toHaveURL(/fstype=xfs/)
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect(state.reads.filter(read => read.path.endsWith('/resource_trends')).at(-1).query).toMatchObject({ metrics: partition, device: '/dev/other', mountpoint: mount.mountpoint, fstype: 'xfs' })
  await heatmap(page).getByTestId('heatmap-cpuBusy').getByRole('button').click()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  const query = state.reads.filter(read => read.path.endsWith('/resource_trends')).at(-1).query
  expect(query.metrics).toBe(cpu)
  expect(query).not.toHaveProperty('device')
  expect(query).not.toHaveProperty('mountpoint')
  values[cpu] = 0; values[memory] = 100
  await refresh(page, locale)
  await expect(heatmap(page).getByTestId('heatmap-cpuBusy').locator('strong')).toHaveText('0.00 %')
  await expect(heatmap(page).getByTestId('heatmap-cpuBusy').getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0')
  await expect(heatmap(page).getByTestId('heatmap-memoryUsed').getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100')
})

test('heatmap never colors stale or missing samples and keeps scalar values during a filesystem outage', async ({ page }) => {
  let stateName = 'valid'
  const state = await resourceBackend(page, { observationResponse: body => {
    for (const line of body.series) if ([cpu, memory].includes(line.metric_key) || line.metric_key.startsWith('node.filesystem.')) {
      if (line.metric_key.startsWith('node.filesystem.') && body.collection.filesystem === 'not_collected') continue
      Object.assign(line.points[0], { data_state: stateName, value: stateName === 'no_data' ? null : line.metric_key.startsWith('node.filesystem.') ? line.points[0].value : 80, sampled_at: stateName === 'no_data' ? null : line.points[0].sampled_at })
    }
  } })
  await page.goto(`/node-resources/${node}?refresh=off`)
  for (const next of ['stale', 'no_data']) {
    stateName = next; await refresh(page)
    for (const id of ['cpuBusy', 'memoryUsed', 'partition']) {
      const tile = heatmap(page).getByTestId(`heatmap-${id}`)
      await expect(tile.locator('strong')).toHaveText('—')
      await expect(tile.getByRole('progressbar')).toHaveCount(0)
      await expect(tile).not.toHaveAttribute('style')
      await expect(tile).toHaveAttribute('data-state', next)
    }
  }
  stateName = 'valid'; state.filesystemMode = 'unavailable'
  await refresh(page)
  await expect(heatmap(page).getByTestId('heatmap-partition')).toHaveCount(0)
  await expect(page.getByTestId('heatmap-filesystem-unavailable')).toBeVisible()
  await expect(heatmap(page).getByTestId('heatmap-cpuBusy').locator('strong')).toHaveText('80.00 %')
  state.filesystemMode = ''
  await refresh(page)
  await expect(heatmap(page).getByTestId('heatmap-partition').locator('strong')).toHaveText('73.68 %')
  state.filesystemMode = 'filesystem-uncollected'
  await refresh(page)
  await expect(heatmap(page).getByTestId('heatmap-partition')).toHaveCount(0)
  await expect(page.getByTestId('heatmap-filesystem-unavailable')).toHaveText('当前采集源未提供文件系统指标。')
})

test('heatmap clears on denial and ignores a prior identity response', async ({ page }) => {
  const state = await resourceBackend(page)
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(heatmap(page).getByRole('progressbar')).toHaveCount(3)
  state.mode = 'denied'; await refresh(page)
  await expect(heatmap(page)).toHaveCount(0)
  state.mode = ''; await refresh(page)
  await expect(heatmap(page).getByRole('progressbar')).toHaveCount(3)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeEnabled()
  state.hold()
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await setIdentity(page, identity([]))
  await expect(heatmap(page)).toHaveCount(0)
  state.release()
  await expect(page.getByRole('alert')).toContainText('无权')
  await expect(heatmap(page)).toHaveCount(0)
})
