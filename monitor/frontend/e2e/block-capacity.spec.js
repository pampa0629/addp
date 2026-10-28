import { expect, test } from '@playwright/test'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { node, resourceBackend } from './node-resources.fixture'

for (const locale of ['zh-cn', 'en']) test(`managed device capacity is lazy, uses IEC units and clears failures (${locale})`, async ({ page }) => {
  let capacityState = 'available', bad = false
  const state = await resourceBackend(page, { locale, observationResponse(body, params) {
    if (params.get('metrics') !== 'node.block.capacity_bytes') return
    body.collection.block_capacity = capacityState
    body.series = ['device', 'mapped'].map((kind, index) => ({ ...body.series[0], dimensions: { device: index ? 'dm-0' : 'vda', kind }, points: body.series[0].points.map(point => ({ ...point, value: 2 * 1024 ** 4 })) }))
    if (bad) body.series[0].dimensions.kind = 'physical'
  } })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/node-resources/${node}?refresh=off`)
  await expect(page.getByTestId('resource-memoryTotal')).toBeVisible()
  expect(state.reads.filter(read => read.query.metrics === 'node.block.capacity_bytes')).toHaveLength(0)
  await page.getByTestId('resource-block-toggle').click()
  const table = page.getByTestId('resource-block-table')
  await expect(table).toContainText('vda')
  await expect(table).toContainText('dm-0')
  await expect(table).toContainText('2.00 TiB')
  await expect(page.getByTestId('resource-block-capacity')).toContainText(locale === 'en' ? 'not a physical host total' : '不代表整机物理总量')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const detail = page.getByTestId('block-capacity-section')
  capacityState = 'stale'
  await detail.getByTestId('resource-detail-refresh').click()
  await expect(detail.getByTestId('resource-block-status')).toContainText(locale === 'en' ? 'stale' : '过期')
  await expect(table).not.toContainText('2.00 TiB')
  capacityState = 'not_collected'
  await detail.getByTestId('resource-detail-refresh').click()
  await expect(detail.getByTestId('resource-block-status')).toContainText(locale === 'en' ? 'disabled' : '未启用')
  capacityState = 'available'; bad = true
  await detail.getByTestId('resource-detail-refresh').click()
  await expect(detail.getByTestId('resource-block-error')).toBeVisible()
  await expect(table).not.toContainText('2.00 TiB')
  bad = false
  await detail.getByTestId('resource-detail-refresh').click()
  await expect(table).toContainText('2.00 TiB')
  await page.screenshot({ path: join(tmpdir(), `addp-block-capacity-${locale}.png`), animations: 'disabled' })
  await page.getByTestId('resource-block-toggle').click()
  await expect(table).toHaveCount(0)
  expect(state.reads.filter(read => read.query.metrics === 'node.block.capacity_bytes')).toHaveLength(5)
})

test('inline capacity shares the same presentation and clears on authorization loss', async ({ page }) => {
  let denied = false
  const state = await resourceBackend(page, { observationResponse(body, params) {
    if (params.get('metrics') !== 'node.block.capacity_bytes') return
    body.collection.block_capacity = 'available'
    body.series[0].dimensions = { device: 'vda', kind: 'unknown' }
  } })
  await page.goto('/node-resources?refresh=off')
  await page.getByTestId(`resource-list-block-${node}`).click()
  const expansion = page.getByTestId('resource-detail-expansion')
  await expect(expansion.getByTestId('resource-block-table')).toContainText('16.00 GiB')
  await page.route('**/api/v1/monitor/platform/resource_observations?**', async route => {
    if (!denied || !route.request().url().includes('node.block.capacity_bytes')) return route.fallback()
    await route.fulfill({ status: 403, json: { error_code: 'permission_denied', error: 'denied' } })
  })
  denied = true
  await expansion.getByTestId('resource-detail-refresh').click()
  await expect(expansion).toHaveCount(0)
  await expect(page.getByTestId('resource-error')).toBeVisible()
  expect(state.reads.some(read => read.query.metrics === 'node.block.capacity_bytes')).toBe(true)
})
