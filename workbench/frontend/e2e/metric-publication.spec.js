import { test, expect } from '@playwright/test'
import { applicationPath, runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

const parameter = (page, label) => page.locator('.parameter-field').filter({ has: page.locator('span').filter({ hasText: new RegExp(`^${label}\\*?$`) }) })
async function choose(page, field, label) {
  await field.locator('.el-select__wrapper').click()
  const list = await field.getByRole('combobox').getAttribute('aria-controls')
  await page.locator(`#${list}`).getByRole('option', { name: label, exact: true }).click()
}
const rows = page => page.getByTestId('runtime-component').first().locator('.el-table__body-wrapper tbody tr')

for (const locale of ['zh-cn', 'en']) {
test(`selection guidance and service names preserve raw IDs (${locale})`, async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true, locale })
  const published = backend.published
  const [metric, directory] = published.snapshot.components
  const nameFields = ['subject_id', 'comparison_id', 'subject_label', 'comparison_label'].map(name => ({ name, type: 'string', nullable: name.endsWith('_label') }))
  backend.descriptors[71].output_contract.fields.push(...nameFields)
  backend.descriptors[71].input_contract.fields.push(...nameFields.map(field => ({ ...field, selectable: true })))
  metric.query_template.select.push(...nameFields.map(field => field.name))
  metric.renderer_config.columns = ['subject_label', 'comparison_label', 'direction', 'value']
  metric.renderer_config.field_presentations = [
    { field: 'subject_label', label: '主体人员' }, { field: 'comparison_label', label: '比较人员' },
    { field: 'direction', label: '方向', value_labels: [{ value: 'forward', label: 'A → B' }, { value: 'reverse', label: 'B → A' }] },
    { field: 'value', label: '重叠率', precision: 6 },
  ]
  for (const [key, label, value] of [['subject_id', '人员 A', 'person-a'], ['comparison_id', '人员 B', 'person-b']]) {
    backend.descriptors[71].input_contract.named_parameters.push({ name: key, type: 'string', required: true })
    metric.parameter_definitions.push({ key, label, control_type: 'text', required: true })
    metric.query_template.named_parameter_bindings.push({ name: key, parameter_key: key })
    published.snapshot.parameters.push({ key, label, control_type: 'text', required: true, default_value: value })
    published.snapshot.parameter_bindings.push({ component_id: metric.id, component_parameter_key: key, application_parameter_key: key })
  }
  const directoryFields = ['person_id', 'nickname'].map(name => ({ name, type: 'string', selectable: true, filterable: name === 'nickname', operators: name === 'nickname' ? ['contains'] : [] }))
  backend.descriptors[72].input_contract = { fields: directoryFields, page: { default_limit: 1, max_limit: 100 }, order: { stable_key: ['person_id'] } }
  backend.descriptors[72].output_contract.fields = directoryFields
  directory.renderer_type = 'table'
  directory.title = '选择人员 A'
  directory.description = '按昵称找到人员后，点击对应行。'
  directory.renderer_config = { columns: ['person_id', 'nickname'] }
  directory.parameter_definitions = []
  directory.query_template = { select: ['person_id', 'nickname'], parameter_filters: [], named_parameter_bindings: [], page_limit: 1, format: 'json', order_by: [{ field: 'person_id', direction: 'asc' }] }
  published.snapshot.parameter_bindings = published.snapshot.parameter_bindings.filter(binding => binding.component_id !== directory.id)
  const nicknameParameter = { key: 'nickname_search', label: '昵称筛选', control_type: 'text', required: false, default_value: '' }
  directory.parameter_definitions.push(nicknameParameter)
  directory.query_template.parameter_filters.push({ parameter_key: nicknameParameter.key, field: 'nickname', operator: 'contains' })
  published.snapshot.parameters.push(nicknameParameter)
  published.snapshot.parameter_bindings.push({ component_id: directory.id, component_parameter_key: nicknameParameter.key, application_parameter_key: nicknameParameter.key })
  published.snapshot.selection_bindings = [{ source_component_id: directory.id, assignments: [{ source_field: 'person_id', application_parameter_key: 'subject_id' }] }]
  const metricRequests = []
  let directoryRequests = 0
  const directoryBodies = []
  await context.route(url => url.pathname.endsWith('/runtime'), route => route.fulfill({ json: published }))
  await context.route('**/api/query/metric_72/query', route => {
    directoryRequests++
    directoryBodies.push(route.request().postDataJSON())
    return route.fulfill({ json: { data: [{ person_id: 'person-c', nickname: '目录旧昵称' }], page: { has_more: true, next_cursor: 'directory-next' } } })
  })
  await context.route('**/api/query/metric_71/query', route => {
    const body = route.request().postDataJSON()
    metricRequests.push(body)
    const id = body.parameters.subject_id
    const label = id === 'person-a' ? '不在目录当前页的人员 A' : '指标服务当前昵称'
    return route.fulfill({ json: { data: [
      { bucket: '2026-01-01', subject_id: id, comparison_id: 'person-b', subject_label: label, comparison_label: null, direction: 'forward', value: 0.25 },
      { bucket: '2026-01-01', subject_id: 'person-b', comparison_id: id, subject_label: null, comparison_label: label, direction: 'reverse', value: 0.5 },
    ], page: { has_more: false, next_cursor: '' } } })
  })
  await page.goto(runtimePath)
  await expect(rows(page)).toHaveCount(2)
  await expect(rows(page).first()).toContainText('不在目录当前页的人员 A')
  await expect(rows(page).first()).toContainText('A → B')
  await expect(rows(page).last()).toContainText('B → A')
  await expect(rows(page).first().getByRole('cell').nth(1)).toHaveText('—')
  const source = page.getByTestId('runtime-component').nth(1)
  await expect(source.getByTestId('component-description')).toHaveText(directory.description)
  await expect(source.getByTestId('selection-hint')).toHaveText(locale === 'en'
    ? 'Click a row to set 人员 A and refresh related results.'
    : '点击一行设置 人员 A，并刷新相关结果。')
  await expect(parameter(page, '人员 B').getByRole('button')).toHaveCount(0)
  const requestCount = metricRequests.length
  await page.setViewportSize({ width: 560, height: 760 })
  await parameter(page, '人员 A').getByRole('button', { name: locale === 'en' ? 'Select from “选择人员 A”' : '从「选择人员 A」选择', exact: true }).click()
  await expect(source).toBeFocused()
  await expect(source).toBeInViewport()
  expect(metricRequests).toHaveLength(requestCount)
  expect(directoryRequests).toBe(1)
  await expect(page.locator('.parameters-card').getByRole('textbox', { name: '昵称筛选', exact: true })).toHaveCount(0)
  const nickname = source.getByRole('textbox', { name: '昵称筛选', exact: true })
  await expect(nickname).toHaveCount(1)
  await source.getByRole('button', { name: locale === 'en' ? 'Next' : '下一页', exact: true }).click()
  await expect.poll(() => directoryBodies.at(-1).page.cursor).toBe('directory-next')
  await nickname.fill('目录')
  await nickname.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  await nickname.dispatchEvent('keydown', { key: 'Enter', keyCode: 229 })
  await nickname.dispatchEvent('keydown', { key: 'Enter', repeat: true })
  expect(directoryRequests).toBe(2)
  await nickname.press('Enter')
  await expect.poll(() => directoryBodies.at(-1).filter).toEqual({ field: 'nickname', op: 'contains', value: '目录' })
  expect(directoryBodies.at(-1).page.cursor).toBe('')
  expect(metricRequests).toHaveLength(requestCount)
  await nickname.fill('')
  await nickname.press('Enter')
  await expect.poll(() => directoryBodies.at(-1).filter).toBeNull()
  expect(directoryRequests).toBe(4)
  await page.getByTestId('runtime-component').nth(1).getByRole('row', { name: 'person-c 目录旧昵称' }).click()
  await expect(page.getByText(locale === 'en' ? 'Parameters updated: 人员 A' : '已更新参数：人员 A', { exact: true })).toBeVisible()
  await expect(parameter(page, '人员 A').getByRole('textbox')).toHaveValue('person-c')
  await expect(rows(page).first().getByRole('cell').first()).toHaveText('指标服务当前昵称')
  await expect(rows(page).last().getByRole('cell').nth(1)).toHaveText('指标服务当前昵称')
  await expect(rows(page).first()).toContainText('0.250000')
  await expect(rows(page).last()).toContainText('0.500000')
  expect(metricRequests.at(-1).parameters).toEqual({ grain: 'total', directions: 'both', subject_id: 'person-c', comparison_id: 'person-b' })
  expect(directoryRequests).toBe(4)
  await parameter(page, '人员 A').getByRole('textbox').fill('person-a')
  await parameter(page, '人员 A').getByRole('textbox').press('Enter')
  await expect(rows(page).first()).toContainText('不在目录当前页的人员 A')
  expect(metricRequests.at(-1).parameters.subject_id).toBe('person-a')
  await expect(source.getByTestId('selection-hint')).toBeVisible()
  published.snapshot.page.visible_sections = ['title', 'query_actions']
  await page.reload()
  await expect(rows(page)).toHaveCount(2)
  await expect(page.locator('.parameter-field')).toHaveCount(0)
  expect(backend.unexpected).toEqual([])
})
}

test('typed value labels persist, reject duplicates and display without changing query parameters', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true })
  await page.goto(applicationPath)
  await page.getByTestId('application-component').first().getByTestId('edit-component-action').click()
  const editor = page.getByTestId('application-component-editor')
  const labels = editor.getByTestId('value-label-editor')
  await labels.getByRole('button', { name: '添加值名称', exact: true }).click()
  await labels.getByRole('textbox', { name: '原值', exact: true }).fill('forward')
  await labels.getByRole('textbox', { name: '显示名称', exact: true }).fill('A → B')
  await labels.getByRole('button', { name: '添加值名称', exact: true }).click()
  await labels.getByRole('textbox', { name: '原值', exact: true }).nth(1).fill('forward')
  await labels.getByRole('textbox', { name: '显示名称', exact: true }).nth(1).fill('B → A')
  await expect(labels.getByRole('alert')).toBeVisible()
  await expect(editor.getByTestId('component-query-action')).toBeDisabled()
  await labels.getByRole('textbox', { name: '原值', exact: true }).nth(1).fill('reverse')
  await expect(labels.getByRole('alert')).toHaveCount(0)
  await page.getByRole('dialog').getByRole('button', { name: '应用组件配置', exact: true }).click()
  await expect(editor).not.toBeVisible()
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect.poll(() => backend.writes.length).toBe(1)
  expect(backend.draft.snapshot.components[0].renderer_config.field_presentations[2].value_labels).toEqual([
    { value: 'forward', label: 'A → B' }, { value: 'reverse', label: 'B → A' },
  ])
  await page.reload()
  await page.getByTestId('application-component').first().getByTestId('edit-component-action').click()
  await expect(labels.getByRole('textbox', { name: '原值', exact: true }).first()).toHaveValue('forward')
  await expect(labels.getByRole('textbox', { name: '显示名称', exact: true }).last()).toHaveValue('B → A')
  await page.getByRole('dialog').getByRole('button', { name: '应用组件配置', exact: true }).click()
  await expect(editor).not.toBeVisible()
  await page.getByRole('button', { name: '发布新修订', exact: true }).click()
  await page.getByRole('button', { name: /^(确定|OK)$/ }).click()
  await expect.poll(() => backend.published.revision_number).toBe(2)
  await page.goto(runtimePath)
  await expect(rows(page)).toHaveCount(2)
  await expect(rows(page).first()).toContainText('A → B')
  await expect(rows(page).last()).toContainText('B → A')
  expect(backend.requests.find(r => r.id === 71).body.parameters).toEqual({ grain: 'total', directions: 'both' })
  expect(backend.unexpected).toEqual([])
})

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

for (const count of [0, 89]) {
test(`shared chart paints count labels and theme text for count ${count}`, async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true })
  const published = backend.published
  Object.assign(published.snapshot.components[1].renderer_config.field_presentations.find(p => p.field === 'value'), { precision: 0, unit: '次' })
  await context.route(`**/data_applications/${published.id}/runtime`, route => route.fulfill({ json: published }))
  await context.route('**/api/query/metric_72/query', route => route.fulfill({ json: {
    data: [{ bucket: '2026-01-01', value: count }, { bucket: '2026-02-01', value: 0 }],
    page: { has_more: false, next_cursor: '' },
  } }))
  // Inspect actual canvas text, including colors and tick labels, rather than only options.
  await context.addInitScript(() => {
    const prototype = CanvasRenderingContext2D.prototype
    const clear = prototype.clearRect, fill = prototype.fillText
    prototype.clearRect = function (...args) {
      this.canvas.__paintedText = []
      return clear.apply(this, args)
    }
    prototype.fillText = function (text, ...args) {
      ;(this.canvas.__paintedText ||= []).push({ text: String(text), color: this.fillStyle })
      return fill.call(this, text, ...args)
    }
  })
  await page.goto(runtimePath)
  const canvas = page.locator('.chart-renderer canvas')
  await expect(canvas).toBeVisible()
  if (count === 0) {
    await expect.poll(() => canvas.evaluate(element => (element.__paintedText || []).filter(item => /^\d+$/.test(item.text)).map(item => item.text))).toEqual(['0', '1'])
  }
  await expect.poll(() => canvas.evaluate(element => (element.__paintedText || []).filter(item => /^\d+ 次$/.test(item.text)).map(item => item.text).sort())).toEqual([`${count} 次`, '0 次'].sort())
  const requestCount = backend.requests.length
  for (const theme of ['', 'dark', 'dark blue', 'dark purple']) {
    await page.locator('html').evaluate((element, value) => { element.className = value }, theme)
    await expect.poll(() => canvas.evaluate((element, count) => {
      const style = getComputedStyle(element)
      const records = element.__paintedText || []
      const matches = (text, variable) => records.some(item => item.text === text && item.color.toLowerCase() === style.getPropertyValue(variable).trim().toLowerCase())
      return matches('0', '--addp-text-secondary') && matches(`${count} 次`, '--addp-text-primary') && matches('0 次', '--addp-text-primary') && matches('次数', '--addp-text-primary')
    }, count)).toBe(true)
  }
  expect(backend.requests).toHaveLength(requestCount)
  expect(backend.unexpected).toEqual([])
})
}

test('optional text contains filters reset pagination and clearing restores an unfiltered request', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true })
  const published = backend.published
  const component = published.snapshot.components[0]
  published.snapshot.components = [component]
  published.snapshot.page.placements = [published.snapshot.page.placements[0]]
  published.snapshot.parameter_bindings = published.snapshot.parameter_bindings.filter(b => b.component_id === component.id)
  component.query_template.parameter_filters = [{ parameter_key: 'nickname', field: 'direction', operator: 'contains' }]
  component.parameter_definitions.push({ key: 'nickname', label: '昵称包含', control_type: 'text', required: false, default_value: '' })
  published.snapshot.parameters.push({ key: 'nickname', label: '昵称包含', control_type: 'text', required: false, default_value: '' })
  published.snapshot.parameter_bindings.push({ application_parameter_key: 'nickname', component_id: component.id, component_parameter_key: 'nickname' })
  const field = backend.descriptors[71].input_contract.fields.find(f => f.name === 'direction')
  field.filterable = true
  field.operators = ['eq', 'contains']
  const requests = []
  await context.route(url => url.pathname.endsWith('/runtime'), route => route.fulfill({ json: published }))
  await context.route(url => url.pathname === '/api/query/metric_71/query', route => {
    const body = route.request().postDataJSON()
    requests.push(body)
    return route.fulfill({ json: { data: Array.from({ length: 50 }, (_, i) => ({ bucket: '2026-01-01', value: i, direction: body.filter?.value || 'all' })), page: { has_more: !body.page.cursor, next_cursor: body.page.cursor ? '' : 'next-page' } } })
  })
  await page.goto(runtimePath)
  await expect(rows(page)).toHaveCount(50)
  const nextPage = page.getByRole('button', { name: '下一页', exact: true })
  await expect.poll(() => nextPage.evaluate(button => {
    const table = button.closest('[data-testid="runtime-component"]').querySelector('.el-table')
    return table.getBoundingClientRect().bottom <= button.getBoundingClientRect().top
  })).toBe(true)
  expect(requests.at(-1).filter).toBeNull()
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect.poll(() => requests.at(-1).page.cursor).toBe('next-page')
  const input = parameter(page, '昵称包含').locator('input')
  await input.fill("苏%_\\'")
  await page.getByTestId('query-all-action').click()
  await expect.poll(() => requests.at(-1).filter).toEqual({ field: 'direction', op: 'contains', value: "苏%_\\'" })
  expect(requests.at(-1).page.cursor || '').toBe('')
  await expect(rows(page).first()).toContainText("苏%_\\'")
  await input.fill('')
  await page.getByTestId('query-all-action').click()
  await expect(rows(page).first()).toContainText('all')
  expect(requests.at(-1).filter).toBeNull()
  expect(requests.at(-1).page.cursor || '').toBe('')
  expect(backend.unexpected).toEqual([])
})
