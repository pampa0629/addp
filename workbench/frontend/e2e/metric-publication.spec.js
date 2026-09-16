import { test, expect } from '@playwright/test'
import { applicationPath, runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

const parameter = (page, label) => page.locator('.parameter-field').filter({ has: page.locator('span').filter({ hasText: new RegExp(`^${label}\\*?$`) }) })
async function choose(page, field, label) {
  await field.locator('.el-select__wrapper').click()
  const list = await field.getByRole('combobox').getAttribute('aria-controls')
  await page.locator(`#${list}`).getByRole('option', { name: label, exact: true }).click()
}
const rows = page => page.getByTestId('runtime-component').first().locator('.el-table__body-wrapper tbody tr')

test('rebound services require explicit component saves and a new immutable application revision', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  backend.rebindServices()
  await page.goto(runtimePath)
  await expect(page.getByTestId('contract-changed-alert')).toHaveCount(2)
  await expect(parameter(page, '统计粒度').locator('input')).toBeDisabled()
  expect(backend.requests).toHaveLength(0)

  await page.goto(applicationPath)
  await expect(page.getByTestId('application-component')).toHaveCount(2)
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(page.getByText('参数值不符合关联组件的服务契约，请检查参数及选项。', { exact: true })).toBeVisible()
  expect(backend.writes).toHaveLength(0)
  for (let i = 0; i < 2; i++) {
    await page.getByTestId('application-component').nth(i).getByTestId('edit-component-action').click()
    const editor = page.getByTestId('application-component-editor')
    await expect(editor.getByTestId('contract-changed-alert')).toBeVisible()
    await expect(editor.getByTestId('component-query-action')).toBeDisabled()
    await page.getByRole('dialog').getByRole('button', { name: '应用组件配置', exact: true }).click()
    await expect(editor).not.toBeVisible()
  }
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect.poll(() => backend.writes.length).toBe(1)
  const saved = backend.draft.snapshot
  expect(saved.parameter_bindings).toEqual(backend.originalPublished.snapshot.parameter_bindings)
  expect(saved.page.placements).toEqual(backend.originalPublished.snapshot.page.placements)
  expect(saved.components.map(c => c.renderer_config)).toEqual(backend.originalPublished.snapshot.components.map(c => c.renderer_config))
  expect(saved.parameters).toEqual(backend.originalPublished.snapshot.parameters)
  expect(JSON.stringify(saved)).not.toContain('"options"')
  expect(backend.published).toEqual(backend.originalPublished)

  await page.getByRole('button', { name: '发布新修订', exact: true }).click()
  await page.getByRole('button', { name: /^(确定|OK)$/ }).click()
  await expect(page.getByText('以下入口均来自已发布修订 2，不会读取未发布草稿。')).toBeVisible()
  expect(backend.writes.map(w => w.action)).toEqual(['save', 'publish'])
  expect(backend.published.snapshot).toEqual(saved)

  await page.goto(runtimePath)
  await expect(page.getByText('发布修订 2', { exact: true })).toBeVisible()
  await expect(rows(page)).toHaveCount(2)
  await expect(page.locator('.chart-renderer canvas')).toBeVisible()
  await expect(parameter(page, '统计粒度')).toContainText('全期')
  await expect(parameter(page, '查询方向')).toContainText('双向')
  await choose(page, parameter(page, '统计粒度'), '按月')
  await page.getByTestId('query-all-action').click()
  await expect(rows(page)).toHaveCount(24)
  await choose(page, parameter(page, '查询方向'), '单向（主体→比较对象）')
  await page.getByTestId('query-all-action').click()
  await expect(rows(page)).toHaveCount(12)
  await expect(rows(page).filter({ hasText: 'reverse' })).toHaveCount(0)
  expect(backend.requests.filter(r => r.id === 71).at(-1).body.parameters).toEqual({ grain: 'month', directions: 'forward' })
  expect(backend.requests.filter(r => r.id === 72).at(-1).body.parameters).toEqual({ grain: 'month' })
  expect(backend.published.snapshot.parameters.map(p => p.default_value)).toEqual(['total', 'both'])
  expect(backend.unexpected).toEqual([])
  expect(errors).toEqual([])
})

test('shared parameter controls enable only after every asynchronous descriptor is ready', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true, deferDescriptors: true })
  try {
    await page.goto(runtimePath)
    await expect.poll(() => backend.pending.size).toBe(2)
    await expect(parameter(page, '统计粒度').locator('input')).toBeDisabled()
    backend.releaseDescriptor(71)
    await expect(parameter(page, '查询方向').getByRole('combobox')).toBeEnabled()
    await expect(parameter(page, '统计粒度').locator('input')).toBeDisabled()
    expect(backend.requests).toHaveLength(0)
    backend.releaseDescriptor(72)
    await expect(parameter(page, '统计粒度').getByRole('combobox')).toBeEnabled()
    await expect(parameter(page, '统计粒度')).toContainText('全期')
    await expect(rows(page)).toHaveCount(2)
    expect(backend.unexpected).toEqual([])
  } finally { backend.releaseAll() }
})

test('English option labels submit the same machine values', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true, locale: 'en' })
  await page.goto(runtimePath)
  await expect(rows(page)).toHaveCount(2)
  await expect(parameter(page, '统计粒度')).toContainText('Total')
  await expect(parameter(page, '查询方向')).toContainText('Both directions')
  await choose(page, parameter(page, '统计粒度'), 'Monthly')
  await choose(page, parameter(page, '查询方向'), 'Forward (subject to comparison)')
  await page.getByTestId('query-all-action').click()
  await expect(rows(page)).toHaveCount(12)
  expect(backend.requests.filter(r => r.id === 71).at(-1).body.parameters).toEqual({ grain: 'month', directions: 'forward' })
  expect(backend.unexpected).toEqual([])
})

test('conflicting options block shared inputs and draft persistence', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true })
  backend.descriptors[71].input_contract.named_parameters[0].options = [{ value: 'total', labels: { 'zh-cn': '全期', en: 'Total' } }]
  backend.descriptors[72].input_contract.named_parameters[0].options = [{ value: 'month', labels: { 'zh-cn': '按月', en: 'Monthly' } }]
  await page.goto(applicationPath)
  await expect(page.getByText('参数契约不可用或存在冲突：双方重叠率, 活动次数', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  expect(backend.writes).toHaveLength(0)
  await page.goto(runtimePath)
  await expect(parameter(page, '统计粒度').locator('input')).toBeDisabled()
  await expect(parameter(page, '统计粒度')).toContainText('活动次数')
  expect(backend.requests).toHaveLength(0)
  expect(backend.unexpected).toEqual([])
})

for (const locale of ['zh-cn', 'en']) {
  test(`frozen parameter captions use ${locale} without overriding application labels`, async ({ page, context }) => {
    const backend = await installMetricApplicationBackend(context, { rebound: true, locale })
    backend.descriptors[71].input_contract.named_parameters[0].presentation = {
      labels: { 'zh-cn': '统计粒度', en: 'Time granularity' },
      descriptions: { 'zh-cn': '无活动月份补零。', en: 'Months without activity return zero.' }
    }
    await page.goto(applicationPath)
    await page.getByTestId('application-component').first().getByTestId('edit-component-action').click()
    const editor = page.getByTestId('application-component-editor')
    await expect(editor.locator('.parameter-caption').first()).toContainText(locale === 'en' ? 'Time granularity' : '统计粒度')
    await expect(editor.locator('.parameter-caption').first()).toContainText(locale === 'en' ? 'Months without activity return zero.' : '无活动月份补零。')
    expect(backend.writes).toHaveLength(0)
  })
}
