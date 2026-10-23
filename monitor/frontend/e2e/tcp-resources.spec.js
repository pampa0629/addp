import { expect, test } from '@playwright/test'
import { node, resourceBackend } from './node-resources.fixture'
const key = 'node.tcp.current_connections'
for (const locale of ['zh-cn','en']) test(`TCP current count and bounded trend keep the host scalar scope (${locale})`, async ({ page }) => {
 let value = 19, missing = false
 const change = body => {
  const line = body.series.find(line => line.metric_key === key)
  if (line) for (const point of line.points) Object.assign(point, { value: missing ? null : value, sampled_at: missing ? null : point.evaluated_at, data_state: missing ? 'no_data' : 'valid' })
 }
 const state = await resourceBackend(page, { locale, observationResponse: change, trendResponse: change })
 await page.goto(`/node-resources/${node}?metric=${key}&range=5m&refresh=off`)
 const card = page.getByTestId('resource-tcpConnections')
 await expect(card.locator('strong')).toHaveText(locale === 'en' ? '19 connections' : '19 条')
 await expect(card).toContainText('ESTABLISHED + CLOSE_WAIT')
 await expect(card).toContainText('TIME_WAIT')
 await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
 const query = state.reads.find(read => read.path.endsWith('/resource_trends')).query
 expect(query).toEqual({ node_id: node, metrics: key, start: '2026-10-06T23:55:00.000Z', end: '2026-10-07T00:00:00.000Z' })
 const refresh = () => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
 value = 0; await refresh()
 await expect(card.locator('strong')).toHaveText(locale === 'en' ? '0 connections' : '0 条')
 missing = true; await refresh()
 await expect(card.locator('strong')).toHaveText('—')
 await expect(page.getByTestId('resource-tcp-unavailable')).toContainText(locale === 'en' ? 'does not mean zero' : '不代表连接数为 0')
 await expect(page.getByTestId('resource-cpuBusy').locator('strong')).toHaveText('0.00 %')
 missing = false; value = 23; await refresh()
 await expect(card.locator('strong')).toHaveText(locale === 'en' ? '23 connections' : '23 条')
})
for (const value of [1.5, -1, 9007199254740992]) test(`invalid TCP count ${value} fails closed`, async ({ page }) => {
 await resourceBackend(page, { observationResponse: body => { const line = body.series.find(line => line.metric_key === key); if (line) line.points[0].value = value } })
 await page.goto(`/node-resources/${node}?refresh=off`)
 await expect(page.getByTestId('resource-error')).toBeVisible()
 await expect(page.getByTestId('resource-tcpConnections')).toHaveCount(0)
})
