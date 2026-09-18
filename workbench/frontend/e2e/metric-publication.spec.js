import { test, expect } from '@playwright/test'
import { applicationPath, runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

const parameter = (page, label) => page.locator('.parameter-field').filter({ has: page.locator('span').filter({ hasText: new RegExp(`^${label}\\*?$`) }) })
async function choose(page, field, label) {
  await field.locator('.el-select__wrapper').click()
  const list = await field.getByRole('combobox').getAttribute('aria-controls')
  await page.locator(`#${list}`).getByRole('option', { name: label, exact: true }).click()
}
const rows = page => page.getByTestId('runtime-component').first().locator('.el-table__body-wrapper tbody tr')

test('monthly metric queries update dates, zero buckets and names together, then restore published defaults', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true, configure(draft, descriptors) {
    for (const [key, label, type, value] of [
      ['subject_id', '人员', 'string', 'person-a'],
      ['range_start', '开始', 'date', '2026-01-01'],
      ['range_end', '结束', 'date', '2027-01-01'],
    ]) {
      const definition = { key, label, control_type: type === 'date' ? 'date' : 'text', required: true }
      draft.snapshot.parameters.push({ ...definition, default_value: value })
      for (const component of draft.snapshot.components) {
        descriptors[component.service_ref.service_id].input_contract.named_parameters.push({ name: key, type, required: true })
        component.parameter_definitions.push(definition)
        component.query_template.named_parameter_bindings.push({ parameter_key: key, name: key })
        draft.snapshot.parameter_bindings.push({ component_id: component.id, component_parameter_key: key, application_parameter_key: key })
      }
    }
    for (const component of draft.snapshot.components) {
      Object.assign(component.renderer_config.field_presentations.find(p => p.field === 'bucket'), {
        temporal_format: 'period', period: { grain_parameter: 'grain', start_parameter: 'range_start', end_parameter: 'range_end' },
      })
    }
    const chart = draft.snapshot.components[1]
    Object.assign(chart.renderer_config, { total_as_value: true, result_name_field: 'subject_label' })
    Object.assign(chart.renderer_config.field_presentations.find(p => p.field === 'value'), { precision: 0, unit: '次' })
    chart.query_template.select.push('subject_label')
    descriptors[72].output_contract.fields.push({ name: 'subject_label', type: 'string' })
    descriptors[72].input_contract.fields.push({ name: 'subject_label', type: 'string', selectable: true })
  } })
  // Fixed Service responses exercise consumer rendering and parameter propagation.
  // Real aggregation, half-open date bounds and division belong to Model's PostgreSQL tests.
  const requests = []
  await context.route(/\/api\/query\/metric_(71|72)\/query$/, route => {
    const id = Number(route.request().url().match(/metric_(\d+)/)[1])
    const body = route.request().postDataJSON()
    requests.push({ id, body })
    const monthly = body.parameters.grain === 'month'
    const subject_label = body.parameters.subject_id === 'person-a' ? '初始人员' : '切换后人员'
    const data = id === 72
      ? (monthly ? [
        { bucket: '2026-06-01', value: 5 }, { bucket: '2026-07-01', value: 0 }, { bucket: '2026-08-01', value: 3 },
      ] : [{ bucket: '2026-01-01', value: 8 }]).map(row => ({ ...row, subject_label }))
      : (monthly ? [
        { bucket: '2026-06-01', direction: 'forward', value: 0.4 },
        { bucket: '2026-06-01', direction: 'reverse', value: 2 / 3 },
        { bucket: '2026-07-01', direction: 'forward', value: 0 },
        { bucket: '2026-07-01', direction: 'reverse', value: 0 },
        { bucket: '2026-08-01', direction: 'forward', value: 0 },
        { bucket: '2026-08-01', direction: 'reverse', value: 0 },
      ] : [
        { bucket: '2026-01-01', direction: 'forward', value: 0.25 },
        { bucket: '2026-01-01', direction: 'reverse', value: 0.5 },
      ])
    return route.fulfill({ json: { data, page: { has_more: false, next_cursor: '' } } })
  })
  await context.addInitScript(() => {
    const fillText = CanvasRenderingContext2D.prototype.fillText
    const clearRect = CanvasRenderingContext2D.prototype.clearRect
    CanvasRenderingContext2D.prototype.clearRect = function (...args) { this.canvas.__metricText = []; return clearRect.apply(this, args) }
    CanvasRenderingContext2D.prototype.fillText = function (text, ...args) { (this.canvas.__metricText ||= []).push(String(text)); return fillText.call(this, text, ...args) }
  })
  await page.goto(runtimePath)
  await expect(page.locator('.value-number')).toHaveText('8')
  await expect(page.getByTestId('result-name')).toContainText('初始人员')

  await parameter(page, '人员').getByRole('textbox').fill('person-c')
  for (const [label, value] of [['开始', '2026-06-01'], ['结束', '2026-09-01']]) {
    await parameter(page, label).getByRole('combobox').fill(value)
    await parameter(page, label).getByRole('combobox').press('Enter')
  }
  await choose(page, parameter(page, '统计粒度'), '按月')
  await expect(page.getByTestId('result-name')).toHaveCount(0)
  await expect(page.locator('.value-number')).toHaveCount(0)
  await page.getByTestId('query-all-action').click()
  await expect(rows(page)).toHaveCount(6)
  await expect(rows(page).filter({ hasText: '2026年6月' }).filter({ hasText: 'forward' })).toContainText('0.400000')
  await expect(rows(page).filter({ hasText: '2026年6月' }).filter({ hasText: 'reverse' })).toContainText('0.666667')
  await expect(rows(page).filter({ hasText: '2026年7月' }).filter({ hasText: '0.000000' })).toHaveCount(2)
  await expect(page.getByTestId('result-name')).toContainText('切换后人员')
  const canvas = page.locator('.chart-renderer canvas')
  await expect.poll(() => canvas.evaluate(el => el.__metricText || [])).toEqual(expect.arrayContaining([
    '2026年6月', '2026年7月', '2026年8月', '5 次', '0 次', '3 次',
  ]))
  for (const id of [71, 72]) {
    expect(requests.filter(request => request.id === id).at(-1).body.parameters).toEqual({
      grain: 'month', subject_id: 'person-c', range_start: '2026-06-01', range_end: '2026-09-01',
      ...(id === 71 ? { directions: 'both' } : {}),
    })
  }
  await expect(page.getByTestId('period-summary')).toHaveCount(2)
  for (const summary of await page.getByTestId('period-summary').all()) {
    await expect(summary).toHaveText('统计期间：2026/06/01（含）至 2026/09/01（不含）')
  }

  await page.getByRole('button', { name: '恢复默认参数', exact: true }).click()
  await expect(page.locator('.value-number')).toHaveText('8')
  await expect(canvas).toHaveCount(0)
  await expect(page.getByTestId('result-name')).toContainText('初始人员')
  await expect(rows(page)).toHaveCount(2)
  await expect(rows(page).first()).toContainText('所选期间合计')
  for (const id of [71, 72]) {
    expect(requests.filter(request => request.id === id).at(-1).body.parameters).toEqual({
      grain: 'total', subject_id: 'person-a', range_start: '2026-01-01', range_end: '2027-01-01',
      ...(id === 71 ? { directions: 'both' } : {}),
    })
  }
  expect(backend.published).toEqual(backend.originalPublished)
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})

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
  await expect(parameter(page, '人员 A').getByRole('textbox')).toHaveCount(0)
  await expect(parameter(page, '人员 A').getByRole('status')).toContainText(locale === 'en' ? 'Selected' : '已选择')
  await expect(rows(page).first().getByRole('cell').first()).toHaveText('指标服务当前昵称')
  await expect(rows(page).last().getByRole('cell').nth(1)).toHaveText('指标服务当前昵称')
  await expect(rows(page).first()).toContainText('0.250000')
  await expect(rows(page).last()).toContainText('0.500000')
  expect(metricRequests.at(-1).parameters).toEqual({ grain: 'total', directions: 'both', subject_id: 'person-c', comparison_id: 'person-b' })
  expect(directoryRequests).toBe(4)
  await page.getByRole('button', { name: locale === 'en' ? 'Restore default parameters' : '恢复默认参数', exact: true }).click()
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
  await editor.locator('[data-field="direction"] summary').click()
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
  await page.getByRole('button', { name: '完成设置', exact: true }).click()
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
  await page.getByRole('button', { name: '筛选条件', exact: true }).click()
  await expect(page.getByText('参数契约不可用或存在冲突：双方重叠率, 活动次数', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '完成设置', exact: true }).click()
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
    await editor.getByRole('tab', { name: locale === 'en' ? '3. Set query inputs' : '3. 设置查询条件' }).click()
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

for (const locale of ['zh-cn', 'en']) {
  test(`table and chart show an explicit reporting period from the completed query (${locale})`, async ({ page, context }) => {
    const backend = await installMetricApplicationBackend(context, { rebound: true, locale })
    const published = backend.published
    for (const [name, label, value] of [['range_start', 'Start', '2026-01-01'], ['range_end', 'End', '2027-01-01']]) {
      published.snapshot.parameters.push({ key: name, label, control_type: 'date', required: true, default_value: value })
      for (const component of published.snapshot.components) {
        backend.descriptors[component.service_ref.service_id].input_contract.named_parameters.push({ name, type: 'date', required: true })
        component.parameter_definitions.push({ key: name, label, control_type: 'date', required: true })
        component.query_template.named_parameter_bindings.push({ parameter_key: name, name })
        published.snapshot.parameter_bindings.push({ application_parameter_key: name, component_id: component.id, component_parameter_key: name })
      }
    }
    for (const component of published.snapshot.components) {
      Object.assign(component.renderer_config.field_presentations.find(p => p.field === 'bucket'), {
        temporal_format: 'period', period: { grain_parameter: 'grain', start_parameter: 'range_start', end_parameter: 'range_end' },
      })
    }
    published.snapshot.components.find(component => component.renderer_type === 'chart').renderer_config.total_as_value = true
    const namedChart = published.snapshot.components.find(component => component.renderer_type === 'chart')
    namedChart.renderer_config.result_name_field = 'current_name'
    namedChart.renderer_config.field_presentations.push({ field: 'current_name', label: 'Person' })
    namedChart.query_template.select.push('current_name')
    backend.descriptors[72].output_contract.fields.push({ name: 'current_name', type: 'string' })
    backend.descriptors[72].input_contract.fields.push({ name: 'current_name', type: 'string', selectable: true })
    await context.route('**/api/query/metric_72/query', route => route.fulfill({ json: { data: [{ bucket: '2026-01-01', value: 12, current_name: 'Current name' }], page: { has_more: false } } }))
    await context.route(`**/data_applications/${published.id}/runtime`, route => route.fulfill({ json: published }))
    await context.addInitScript(() => {
      const original = CanvasRenderingContext2D.prototype.fillText
      const clear = CanvasRenderingContext2D.prototype.clearRect
      CanvasRenderingContext2D.prototype.clearRect = function (...args) { this.canvas.__periodTexts = []; return clear.apply(this, args) }
      CanvasRenderingContext2D.prototype.fillText = function (text, ...args) { (this.canvas.__periodTexts ||= []).push(String(text)); return original.call(this, text, ...args) }
    })
    await page.goto(runtimePath)
    const total = locale === 'en' ? 'Selected period total' : '所选期间合计'
    const month = locale === 'en' ? 'January 2026' : '2026年1月'
    const canvas = page.locator('.chart-renderer canvas')
    await expect(rows(page).first()).toContainText(total)
    await expect(page.getByTestId('period-summary')).toHaveCount(2)
    await expect(page.getByTestId('period-summary').first()).toContainText(locale === 'en' ? '(exclusive)' : '（不含）')
    await expect(canvas).toHaveCount(0)
    await expect(page.locator('.scalar-value-renderer .value-number')).toHaveText('12.000000')
    await expect(page.getByTestId('result-name')).toContainText('Current name')
    const metricCard = page.getByTestId('runtime-component').nth(1)
    // A table beside a total card still owns the original fixed-height row.
    expect((await metricCard.boundingBox()).height).toBe(444)
    await expect(metricCard).not.toHaveClass(/runtime-component--content/)
    published.snapshot.page.placements[1].y = 6
    published.snapshot.page.placements[1].x = 0
    published.snapshot.page.placements[1].width = 12
    await page.reload()
    await expect(metricCard).toHaveClass(/runtime-component--content/)
    expect((await metricCard.boundingBox()).height).toBeLessThan(350)
    const tableBounds = await page.getByTestId('runtime-component').first().boundingBox()
    expect((await metricCard.boundingBox()).y).toBeCloseTo(tableBounds.y + tableBounds.height + 12, 0)
    await choose(page, parameter(page, '统计粒度'), locale === 'en' ? 'Monthly' : '按月')
    await expect(page.getByTestId('period-summary')).toHaveCount(0)
    await expect(page.locator('.scalar-value-renderer')).toHaveCount(0)
    await expect(page.getByTestId('result-name')).toHaveCount(0)
    await expect(metricCard).not.toHaveClass(/runtime-component--content/)
    expect((await metricCard.boundingBox()).height).toBe(444)
    await page.getByTestId('query-all-action').click()
    await expect(rows(page).first()).toContainText(month)
    await expect.poll(() => canvas.evaluate(el => el.__periodTexts || [])).toContain(month)
    await expect(page.getByTestId('result-name')).toContainText('Current name')
    expect(backend.requests.at(-1).body.parameters.range_start).toBe('2026-01-01')
    expect(backend.requests.at(-1).body.order_by).toContainEqual({ field: 'bucket', direction: 'asc' })
    await choose(page, parameter(page, '统计粒度'), locale === 'en' ? 'Total' : '全期')
    let totalRows = [{ bucket: '2026-01-01', value: 0 }]
    let hasMore = false
    await context.route('**/api/query/metric_72/query', route => route.fulfill({ json: { data: totalRows, page: { has_more: hasMore } } }))
    await page.getByTestId('query-all-action').click()
    await expect(page.locator('.value-number')).toHaveText('0.000000')
    await expect(canvas).toHaveCount(0)
    await expect(page.getByTestId('result-name')).toContainText(locale === 'en' ? 'Name not provided' : '未提供名称')
    totalRows = [{ bucket: '2026-01-01', value: 0, current_name: 'Another person' }]
    await page.getByTestId('query-all-action').click()
    await expect(page.getByTestId('result-name')).toContainText('Another person')
    totalRows = [{ value: 1, current_name: 'One' }, { value: 2, current_name: 'Two' }]
    await page.getByTestId('query-all-action').click()
    await expect(page.getByTestId('result-name')).toContainText(locale === 'en' ? 'Cannot determine a single name' : '无法确定单一名称')
    for (const [data, partial] of [[[], false], [[{value:1},{value:2}], false], [[{value:null}], false], [[{value:3}], true]]) {
      totalRows = data
      hasMore = partial
      await page.getByTestId('query-all-action').click()
      await expect(page.getByTestId('runtime-component').nth(1).getByRole('alert')).toBeVisible()
      await expect(page.locator('.scalar-value-renderer')).toHaveCount(0)
      await expect(canvas).toHaveCount(0)
    }
    // A total card selects the original service row, never its formatted date label.
    const chartComponent = published.snapshot.components.find(component => component.renderer_type === 'chart')
    published.snapshot.selection_bindings = [{ source_component_id: chartComponent.id, assignments: [{ source_field: 'bucket', application_parameter_key: 'range_start' }] }]
    totalRows = [{ bucket: '2026-02-01', value: 0 }]
    hasMore = false
    await page.reload()
    const card = page.locator('.scalar-value-renderer [role="button"]')
    await expect(card).toBeVisible()
    await card.press('Enter')
    await expect(parameter(page, 'Start').getByRole('combobox')).toHaveCount(0)
    await expect.poll(() => backend.requests.at(-1).body.parameters.range_start).toBe('2026-02-01')
    // Content height includes long labels/descriptions and wrapped multiple values.
    chartComponent.description = '活动次数与当前选择的统计期间。 '.repeat(16)
    chartComponent.renderer_config.measures = ['value', 'second_value', 'third_value', 'fourth_value']
    chartComponent.renderer_config.field_presentations.push(...['second_value', 'third_value', 'fourth_value'].map(field => ({ field, label: field, precision: 0 })))
    totalRows = [{ bucket: '2026-02-01', value: 0, second_value: 1, third_value: 2, fourth_value: 3 }]
    await page.setViewportSize({ width: 390, height: 844 })
    await page.reload()
    await expect(metricCard.locator('.value-number')).toHaveCount(4)
    await expect(metricCard).toHaveClass(/runtime-component--content/)
    const overflow = await metricCard.evaluate(el => {
      const body = el.querySelector('.el-card__body')
      const bounds = el.getBoundingClientRect()
      const last = el.querySelector('.value-card:last-child').getBoundingClientRect()
      return { vertical: body.scrollHeight - body.clientHeight, horizontal: el.scrollWidth - el.clientWidth, bottom: last.bottom - bounds.bottom }
    })
    expect(overflow.vertical).toBeLessThanOrEqual(1)
    expect(overflow.horizontal).toBeLessThanOrEqual(1)
    expect(overflow.bottom).toBeLessThan(0)
    await page.setViewportSize({ width: 1280, height: 900 })
    published.snapshot.page.display_mode = 'wallboard'
    await page.reload()
    await expect(metricCard.locator('.value-number')).toHaveCount(4)
    await expect(metricCard).not.toHaveClass(/runtime-component--content/)
    expect(backend.writes).toHaveLength(0)
    expect(backend.unexpected).toEqual([])
  })
}

for (const locale of ['zh-cn', 'en']) {
  test(`explicit result name field is selected, saved and restored (${locale})`, async ({ page, context }) => {
    const backend = await installMetricApplicationBackend(context, { rebound: true, locale, configure(draft, descriptors) {
      descriptors[72].output_contract.fields.push({ name: 'current_name', type: 'string', comment: 'Person' })
      descriptors[72].input_contract.fields.push({ name: 'current_name', type: 'string', selectable: true })
    } })
    await page.goto(applicationPath)
    await page.getByTestId('application-component').nth(1).getByTestId('edit-component-action').click()
    const editor = page.getByTestId('application-component-editor')
    const label = locale === 'en' ? 'Result name field (optional)' : '结果名称字段（可选）'
    await choose(page, editor.locator('.el-form-item').filter({ has: page.getByText(label, { exact: true }) }), 'Person')
    await page.getByRole('dialog').getByRole('button', { name: locale === 'en' ? 'Apply component configuration' : '应用组件配置', exact: true }).click()
    await expect(editor).not.toBeVisible()
    await page.getByRole('button', { name: locale === 'en' ? 'Save draft' : '保存草稿', exact: true }).click()
    await expect.poll(() => backend.writes.length).toBe(1)
    const saved = backend.draft.snapshot.components[1]
    expect(saved.renderer_config.result_name_field).toBe('current_name')
    expect(saved.query_template.select).toContain('current_name')
    expect(saved.renderer_config.field_presentations).toContainEqual({ field: 'current_name', label: 'Person' })
    expect(backend.published).toEqual(backend.originalPublished)
    await page.reload()
    await page.getByTestId('application-component').nth(1).getByTestId('edit-component-action').click()
    await expect(editor.locator('.el-form-item').filter({ has: page.getByText(label, { exact: true }) })).toContainText('Person')
    expect(backend.unexpected).toEqual([])
  })
}
