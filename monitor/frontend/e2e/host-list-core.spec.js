import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { resourceBackend } from './node-resources.fixture'

for (const locale of ['zh-cn', 'en']) test(`twenty-host core list reads five exact metrics once and retains independent states (${locale})`, async ({ page }) => {
  const nodes = Array.from({ length: 20 }, (_, i) => ({ node_id: `12300000-0000-4000-8000-${String(i + 1).padStart(12, '0')}`, version: 1, display_name: `Host ${i + 1}`, enabled: true, addresses: [`192.0.2.${i + 1}`] }))
  let stale = false, incomplete = false
  const state = await resourceBackend(page, { locale, nodes, summaryResponse: body => {
    for (const [i, row] of body.data.entries()) for (const line of row.series) {
      const values = { 'node.cpu.logical_cores': 18 + i, 'node.cpu.busy_percent': 12.34, 'node.memory.total_bytes': 32 * 1024 ** 3, 'node.memory.used_percent': 37.06, 'node.uptime_seconds': 4 * 86400 + 2 * 3600 + 26 * 60 }
      line.points[0].value = values[line.metric_key]
      if (stale && line.metric_key === 'node.cpu.busy_percent') Object.assign(line.points[0], { data_state: 'stale', sampled_at: new Date(Date.parse(row.end) - 61000).toISOString() })
    }
    body.data.reverse()
    if (incomplete) body.data[0].series.pop()
  } })
  await page.setViewportSize({ width: 1480, height: 900 })
  await page.goto('/node-resources?refresh=off')
  const list = page.getByTestId('resource-node-list')
  await expect(list.locator('strong')).toHaveCount(100)
  await expect(list.getByTestId('resource-summary-cores').first()).toHaveText(locale === 'en' ? '18 cores' : '18 核')
  await expect(list.getByTestId('resource-summary-cores').last()).toHaveText(locale === 'en' ? '37 cores' : '37 核')
  await expect(list.getByTestId('resource-summary-cpuBusy').first()).toHaveText('12.34 %')
  await expect(list.getByTestId('resource-summary-memoryTotal').first()).toHaveText('32.00 GiB')
  await expect(list.getByTestId('resource-summary-memoryUsed').first()).toHaveText('37.06 %')
  await expect(list.getByTestId('resource-summary-uptime').first()).toHaveText(locale === 'en' ? '4 days 2 hours 26 minutes' : '4天 2小时 26分钟')
  const summary = state.reads.filter(read => read.path.endsWith('/resource_summaries'))
  expect(summary).toHaveLength(1)
  expect(summary[0].query).toEqual({ node_ids: nodes.map(node => node.node_id).join(','), metrics: 'node.cpu.logical_cores,node.cpu.busy_percent,node.memory.total_bytes,node.memory.used_percent,node.uptime_seconds' })
  expect(state.reads.find(read => read.path.endsWith('/host_nodes')).query.page_size).toBe('20')
  expect(state.reads.filter(read => /resource_observations|resource_trends/.test(read.path))).toHaveLength(0)
  const shot = join(mkdtempSync(join(tmpdir(), 'addp-host-list-core-')), `${locale}.png`)
  await page.screenshot({ path: shot, animations: 'disabled' })
  console.log(`Host core list screenshot: ${shot}`)
  const refresh = () => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  stale = true; await refresh()
  await expect(list.getByTestId('resource-summary-cpuBusy').first()).toHaveText('—')
  await expect(list.getByTestId('resource-summary-memoryTotal').first()).toHaveText('32.00 GiB')
  incomplete = true; await refresh()
  await expect(page.getByTestId('resource-summary-error')).toBeVisible()
  await expect(page.getByRole('button', { name: locale === 'en' ? 'View resources' : '查看资源', exact: true })).toHaveCount(20)
  expect((await list.locator('strong').allTextContents()).every(value => value === '—')).toBe(true)
  stale = false; incomplete = false; await refresh()
  await expect(list.getByTestId('resource-summary-cpuBusy').first()).toHaveText('12.34 %')
})
