import { expect, test } from '@playwright/test'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { backend, identity, permissions, id, node, setIdentity } from './monitoring-targets.fixture'
import { observations } from './resource-observations.fixture'
import { summaryMetrics } from '../src/utils/nodeResources'

const observing = () => identity([...permissions, 'monitor.resource_observation.read'])
const cell = page => page.getByTestId(`target-list-collection-${id}`)
const secondNode = '15daeb87-b7bf-434b-a8ea-598c82e266d4'
const otherTarget = '80ff1d2a-213b-4cec-99a1-15458c463bb9'
const disabledTarget = 'a0e9964c-e2d3-46ba-b151-52249cff528e'
const containerTarget = '626089da-5030-4ce5-9ce9-13c791f562f4'
const target = (targetID, hostID, kind = 'host_resources', enabled = true) => ({ id: targetID, version: 1, subject: { kind: 'node', node_id: hostID }, monitor_kind: kind, enabled, source: { type: kind === 'host_resources' ? 'node_exporter' : 'cadvisor', endpoint: 'https://node.test:9443/metrics' } })

for (const locale of ['zh-cn','en']) test(`mixed list has one bounded observation and separates intent from evidence (${locale})`, async ({ page }) => {
  const state = await backend(page, { identity: observing(), locale, total: 4,
    hosts: [node, secondNode].map((node_id, i) => ({ node_id, display_name: `Host ${i}`, version: 1, enabled: true, addresses: ['node.test'] })),
    additionalTargets: [target(otherTarget, secondNode), target(disabledTarget, node, 'host_resources', false), target(containerTarget, node, 'container_resources')] })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText(locale === 'en' ? 'Latest scrape succeeded' : '最近抓取成功')
  await expect(page.getByTestId(`target-list-collection-${otherTarget}`)).toContainText(locale === 'en' ? 'Latest scrape succeeded' : '最近抓取成功')
  await expect(page.getByTestId(`target-list-collection-${disabledTarget}`)).toContainText(locale === 'en' ? 'Collection disabled' : '已停用采集')
  await expect(page.getByTestId(`target-list-collection-${containerTarget}`)).toContainText(locale === 'en' ? 'Container results unsupported' : '暂不支持容器采集结果')
  await expect(cell(page)).toContainText('2026')
  expect(state.summaryReads).toEqual([{ node_ids: `${node},${secondNode}` }])
  expect(state.collectionReads).toHaveLength(0)
  expect(state.writes).toHaveLength(0)
  await page.setViewportSize({ width: 620, height: 900 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(cell(page).locator('.el-tag')).toBeInViewport({ ratio: 1 })
  await page.screenshot({ path: join(tmpdir(), `addp-target-collection-list-${locale}.png`), animations: 'disabled' })
})

for (const [mode, text, hasTime] of [['failed','最近抓取失败',true],['stale','采集证据已过期',true],['no_sample','暂无抓取证据',false],['not_connected','主机未接入采集',false]]) test(`list exposes ${mode} without a fabricated metric`, async ({ page }) => {
  const state = await backend(page, { identity: observing(), summaryMode: mode })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText(text)
  await expect(cell(page).locator('.target-sample-time')).toHaveCount(hasTime ? 1 : 0)
  await expect(cell(page)).not.toContainText('%')
  expect(state.summaryReads).toHaveLength(1)
})

for (const mode of ['unavailable','denied','invalid']) test(`failed ${mode} refresh clears prior success without blocking settings`, async ({ page }) => {
  const state = await backend(page, { identity: observing() })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText('最近抓取成功')
  state.setSummaryMode(mode)
  await page.getByRole('button', { name: '刷新列表', exact: true }).click()
  await expect(page.getByTestId('target-list-collection-error')).toBeVisible()
  await expect(cell(page)).toContainText('采集结果不可用')
  await expect(cell(page)).not.toContainText('最近抓取成功')
  await expect(cell(page).locator('.target-sample-time')).toHaveCount(0)
  await page.getByRole('button', { name: '详情', exact: true }).click()
  await expect(page.getByTestId('target-endpoint')).toBeEditable()
  expect(state.writes).toHaveLength(0)
})

for (const mode of ['wrong-target','wrong-version']) test(`list rejects ${mode} evidence and recovers through refresh`, async ({ page }) => {
  const state = await backend(page, { identity: observing(), summaryMode: mode })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText('配置已变化，请刷新列表')
  await expect(cell(page)).not.toContainText('最近抓取成功')
  state.setSummaryMode('collecting')
  await page.getByRole('button', { name: '刷新列表', exact: true }).click()
  await expect(cell(page)).toContainText('最近抓取成功')
  expect(state.summaryReads).toHaveLength(2)
})

test('budget rejection allows reducing page size without splitting or per-row queries', async ({ page }) => {
  const targetLists = []; page.on('request', req => { const url = new URL(req.url()); if (url.pathname.endsWith('/monitoring_targets')) targetLists.push(Object.fromEntries(url.searchParams)) })
  const state = await backend(page, { identity: observing(), total: 45, summaryMode: 'budget' })
  await page.goto('/monitoring-targets?page=2')
  await expect(page.getByTestId('target-list-collection-error')).toContainText('请减少每页数量')
  expect(state.summaryReads).toHaveLength(1)
  state.setSummaryMode('collecting')
  await page.locator('.el-pagination__sizes .el-select').click()
  await page.getByRole('option', { name: /^10/ }).click()
  await expect(cell(page)).toContainText('最近抓取成功')
  expect(new URL(page.url()).searchParams.get('page_size')).toBe('10')
  expect(new URL(page.url()).searchParams.has('page')).toBe(false)
  expect(targetLists.at(-1)).toMatchObject({ page: '1', page_size: '10' })
  expect(state.summaryReads).toHaveLength(2)
  expect(state.collectionReads).toHaveLength(0)
})

for (const [name, options, expected] of [
  ['no observation permission', {}, '当前身份无权查看采集结果'],
  ['disabled target', { identity: observing(), enabled: false }, '已停用采集'],
  ['container target', { identity: observing(), kind: 'container_resources' }, '暂不支持容器采集结果'],
  ['unreadable host', { identity: observing(), hostUnavailable: true }, '主机不可读取']
]) test(`${name} keeps configuration but makes no observation request`, async ({ page }) => {
  const state = await backend(page, options)
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText(expected)
  await expect(page.getByRole('button', { name: '详情', exact: true })).toBeVisible()
  expect(state.summaryReads).toHaveLength(0)
  expect(state.collectionReads).toHaveLength(0)
})

test('empty lists do not query observations', async ({ page }) => {
  const state = await backend(page, { identity: observing(), empty: true })
  await page.goto('/monitoring-targets')
  await expect(page.getByText('暂无监测目标', { exact: true })).toBeVisible()
  expect(state.summaryReads).toHaveLength(0)
})

test('revocation discards a held list observation while retaining configuration access', async ({ page }) => {
  const state = await backend(page, { identity: observing(), holdSummary: true })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText('正在读取采集结果')
  await setIdentity(page, identity())
  await expect(cell(page)).toContainText('当前身份无权查看采集结果')
  state.releaseSummary()
  await expect(cell(page)).not.toContainText('最近抓取成功')
  expect(state.summaryReads).toHaveLength(1)
})

test('a prior page response cannot overwrite a newer failed observation', async ({ page }) => {
  const state = await backend(page, { identity: observing(), total: 45, summaryMode: 'failed' })
  let release, first = true
  const barrier = new Promise(resolve => { release = resolve })
  await page.route('**/api/v1/monitor/platform/resource_summaries?**', async route => {
    if (!first) return route.fallback()
    first = false
    await barrier
    const value = observations(node, summaryMetrics.map(row => row.key), false, '', '2026-10-09T00:00:00Z')
    value.target_id = id; value.target_saved_version = 1
    await route.fulfill({ json: { data: [value] } })
  })
  await page.goto('/monitoring-targets')
  await expect(cell(page)).toContainText('正在读取采集结果')
  await page.locator('.el-pagination .btn-next').click()
  await expect(cell(page)).toContainText('最近抓取失败')
  release()
  await page.evaluate(() => new Promise(requestAnimationFrame))
  await expect(cell(page)).toContainText('最近抓取失败')
  expect(state.summaryReads).toHaveLength(1)
})
