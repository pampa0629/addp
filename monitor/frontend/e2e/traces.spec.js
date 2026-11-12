import { test, expect } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'
import { tracesBackend as backend, tracePermissions as keys, traceQuery as query } from './traces.fixture'

test('directory module choices apply immediately and clear the old trace', async ({ page }) => {
  const state = await backend(page, { identity: identity([...keys, 'platform.module_log.read']) })
  await page.goto('/traces?'+query())
  const spans = page.getByTestId('trace-spans')
  await expect(spans.getByRole('button', { name: '关联日志', exact: true }).first()).toBeVisible()
  await expect(spans).toContainText('/items/:id')
  await page.getByRole('combobox', { name: '服务模块' }).press('ArrowDown')
  await page.getByRole('option', { name: 'system', exact: true }).click()
  await expect(page.getByTestId('trace-instances')).toContainText('system')
  await expect(spans).toHaveCount(0)
  await expect(page.getByTestId('trace-search').getByRole('textbox', { name: 'Trace ID' })).toBeVisible()
  expect(state.instanceCalls).toHaveLength(2)
  expect(state.instanceCalls.at(-1)).toEqual({ page: '1', page_size: '10', role: 'backend', module_name: 'system' })
  await expect.poll(() => state.calls.slice(1)).toEqual([{ instance_id: '2' }])
  await page.reload()
  await expect(page.getByTestId('trace-instances')).toContainText('system')
  await expect(page.getByRole('combobox', { name: '观测层' })).toHaveCount(0)
  await page.goto('/traces?module_name=system&role=ingress')
  await expect.poll(() => state.instanceCalls.at(-1)).toEqual({ page: '1', page_size: '10', role: 'ingress', module_name: 'system' })
  await page.getByRole('combobox', { name: '服务模块' }).press('ArrowDown')
  await page.getByRole('option', { name: 'monitor', exact: true }).click()
  await expect(page.getByTestId('trace-instances')).toContainText('monitor')
  expect(state.instanceCalls.at(-1)).toEqual({ page: '1', page_size: '10', role: 'backend', module_name: 'monitor' })
  await expect(page).toHaveURL(url => !url.searchParams.has('role'))
})

test('exact ID search keeps the form after a miss and can retry without an instance', async ({ page }) => {
  const state = await backend(page)
  const id = '1'.repeat(32)
  await page.goto('/traces?' + query())
  const form = page.getByTestId('trace-search')
  const input = form.getByRole('textbox', { name: 'Trace ID' })
  const refresh = page.getByRole('button', { name: '刷新', exact: true })
  const spans = page.getByTestId('trace-spans')
  await expect(spans).toContainText('/items/:id')
  state.mode('missing')
  await refresh.click()
  await expect(page.getByTestId('trace-error')).toContainText('当前条件下没有可读的已保留调用链')
  await expect(page.getByTestId('trace-instances')).toContainText('monitor')
  await expect(input).toHaveValue(id)
  await expect(spans).toHaveCount(0)

  await page.getByRole('button', { name: '返回调用链列表' }).click()
  await expect(page.getByTestId('trace-results')).toBeVisible()
  const instanceReads = state.instanceCalls.length
  await input.fill(id.slice(0, 31))
  await expect(form.getByRole('button', { name: '搜索', exact: true })).toHaveCount(0)
  await expect(page.getByText('请输入完整的 32 位 Trace ID')).toBeVisible()
  expect(state.calls.at(-1)).toHaveProperty('instance_id', '1')
  await input.fill(id)
  await expect(page).toHaveURL(url => url.searchParams.get('trace_id') === id && !url.searchParams.has('instance_id'))
  await expect(page.getByTestId('trace-error')).toContainText('当前条件下没有可读的已保留调用链')
  await expect(input).toHaveValue(id)
  expect(state.instanceCalls).toHaveLength(instanceReads)
  expect(state.calls.at(-1)).not.toHaveProperty('instance_id')

  state.mode('failed')
  await refresh.click()
  await expect(page.getByTestId('trace-error')).toContainText('调用链服务不可用')
  await expect(input).toHaveValue(id)
  state.mode('valid')
  await refresh.click()
  await expect(spans).toContainText('/items/:id')
  expect(state.instanceCalls).toHaveLength(instanceReads)
})

test('one start time applies a bounded window and returning from detail restores the list', async ({ page }) => {
  const now = new Date(Math.floor(Date.now() / 1000) * 1000 - 1000)
  await page.clock.setFixedTime(now)
  const state = await backend(page)
  await page.goto('/traces?module_name=monitor')
  const form = page.getByTestId('trace-search')
  const start = form.getByRole('combobox', { name: '开始时间' })
  await expect(start).toHaveAttribute('placeholder', '选择历史开始时间')
  await expect.poll(() => state.calls.at(-1)).toEqual({ instance_id: '1' })
  await expect(page.getByRole('button', { name: '查询调用链', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('trace-results')).toContainText('1'.repeat(32))

  async function chooseStart(minutesAgo) {
    const selected = new Date(now.getTime() - minutesAgo * 60_000)
    const localTime = await page.evaluate(iso => new Date(iso).toLocaleString('sv-SE'), selected.toISOString())
    await start.fill(localTime)
    await start.press('Enter')
    return selected.toISOString()
  }
  const olderStart = await chooseStart(30)
  await expect.poll(() => state.calls.at(-1)).toEqual({
    instance_id: '1', start: olderStart, end: new Date(now.getTime() - 15 * 60_000).toISOString()
  })
  const windowQuery = { instance_id: '1', start: await chooseStart(5), end: now.toISOString() }
  await expect.poll(() => state.calls.at(-1)).toEqual(windowQuery)
  await expect(form).toContainText('查至')

  const results = page.getByTestId('trace-results')
  await results.getByRole('button', { name: '查看调用链' }).click()
  await expect(page.getByTestId('trace-spans')).toBeVisible()
  await page.goBack()
  await expect(results.getByRole('button', { name: '查看调用链' })).toBeVisible()
  await results.getByRole('button', { name: '查看调用链' }).click()
  await expect(page.getByTestId('trace-spans')).toBeVisible()
  await page.locator('header').getByRole('button', { name: '返回调用链列表' }).click()
  await expect(results.getByRole('button', { name: '查看调用链' })).toBeVisible()
  await expect.poll(() => state.calls.at(-1)).toEqual(windowQuery)
  await expect(page).toHaveURL(url => !url.searchParams.has('trace_id') && url.searchParams.get('start') === windowQuery.start)
  await start.fill('')
  await start.press('Enter')
  await expect.poll(() => state.calls.at(-1)).toEqual({ instance_id: '1' })
  await expect(form).not.toContainText('查至')
})

test('recent presets query immediately and restore a moving window after reload and detail navigation', async ({ page }) => {
  const now = Math.floor(Date.now() / 1000) * 1000 - 120_000
  await page.clock.setFixedTime(now)
  const state = await backend(page)
  await page.goto('/traces?module_name=monitor')
  const preset = page.getByRole('combobox', { name: '查询时段' })
  const results = page.getByTestId('trace-results')
  await expect(results.getByRole('button', { name: '查看调用链' })).toBeVisible()
  async function choose(name) {
    await preset.press('ArrowDown')
    await page.getByRole('option', { name, exact: true }).click()
  }
  async function expectWindow(end, minutes) {
    await expect.poll(() => state.calls.at(-1)).toEqual({
      instance_id: '1',
      start: new Date(end - minutes * 60_000).toISOString(),
      end: new Date(end).toISOString()
    })
  }
  await choose('最近 30 分钟')
  await expectWindow(now, 30)
  await expect(page).toHaveURL(url => url.searchParams.get('range') === '30m' && !url.searchParams.has('start'))
  await page.clock.setFixedTime(now + 60_000)
  await page.reload()
  await expectWindow(now + 60_000, 30)
  await choose('最近 1 小时')
  await expectWindow(now + 60_000, 60)
  await results.getByRole('button', { name: '查看调用链' }).click()
  await expect(page.getByTestId('trace-spans')).toBeVisible()
  await page.getByRole('button', { name: '返回调用链列表' }).click()
  await expect(results.getByRole('button', { name: '查看调用链' })).toBeVisible()
  await expect(page).toHaveURL(url => url.searchParams.get('range') === '1h' && !url.searchParams.has('trace_id'))
  await choose('最近 15 分钟')
  await expect.poll(() => state.calls.at(-1)).toEqual({ instance_id: '1' })
  await expect(page).toHaveURL(url => !url.searchParams.has('range') && !url.searchParams.has('start'))
})

test('a module with multiple instances queries only the explicitly selected instance', async ({ page }) => {
  const state = await backend(page, { multiple: true })
  await page.goto('/traces?module_name=monitor')
  const instances = page.getByTestId('trace-instances')
  await expect(instances.getByRole('button', { name: 'three', exact: true })).toBeVisible()
  await expect(page.getByTestId('trace-results')).toHaveCount(0)
  expect(state.calls).toEqual([])
  await instances.getByRole('button', { name: 'one', exact: true }).click()
  await expect(page.getByTestId('trace-results')).toContainText('1'.repeat(32))
  const { start, end } = state.sampleCalls[0]
  expect(state.calls).toEqual([{ instance_id: '1', start, end }])
})

for (const locale of ['zh-cn', 'en']) test(`sample states describe only the current window and opening a row freezes it (${locale})`, async ({ page }) => {
  const now = Math.floor(Date.now() / 1000) * 1000 - 120_000
  await page.clock.setFixedTime(now)
  const state = await backend(page, { multiple: true, locale })
  await page.route('**/api/v1/monitor/platform/traces/samples?**', route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    state.sampleCalls.push(params)
    return route.fulfill({ json: { ...params, queried_at: new Date(now).toISOString(), policy_version: 2, coverage: 'sampled',
      data: [{ instance_id: 1, state: 'has_samples' }, { instance_id: 3, state: 'not_found' }] } })
  })
  await page.goto('/traces?range=1h')
  const cells = page.getByTestId('trace-sample-state')
  await expect(cells.nth(0)).toHaveText(locale === 'en' ? 'Sample found' : '有样本')
  await expect(cells.nth(1)).toHaveText(locale === 'en' ? 'None found in this window' : '本时段未发现')
  expect(state.calls).toHaveLength(0)
  expect(state.sampleCalls).toEqual([{ instance_ids: '1,3', start: new Date(now - 3600000).toISOString(), end: new Date(now).toISOString() }])
  await page.clock.setFixedTime(now + 120_000)
  await page.getByTestId('trace-instances').getByRole('button', { name: 'one', exact: true }).click()
  await expect(page.getByTestId('trace-results')).toContainText('1'.repeat(32))
  const { start, end } = state.sampleCalls[0]
  expect(state.calls.at(-1)).toEqual({ instance_id: '1', start, end })
  await expect(page).toHaveURL(url => url.searchParams.get('end') === end && !url.searchParams.has('range'))
})

test('failed sample checks remain unknown and current permission denial clears the list', async ({ page }) => {
  await backend(page, { multiple: true })
  let denied = false
  await page.route('**/api/v1/monitor/platform/traces/samples?**', route =>
    route.fulfill({ status: denied ? 403 : 503, json: { error_code: denied ? 'permission_denied' : 'trace_backend_unavailable' } }))
  await page.goto('/traces')
  const cells = page.getByTestId('trace-sample-state')
  await expect(cells).toHaveText(['暂无法确认', '暂无法确认'])
  await expect(page.getByTestId('trace-instances')).toContainText('one')
  denied = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('trace-error')).toContainText('没有调用链读取权限')
  await expect(page.getByTestId('trace-instances')).not.toContainText('one')
  await expect(cells).toHaveCount(0)
})

test('paging cancels sample checks and late results cannot overwrite the new page', async ({ page }) => {
  await backend(page)
  await page.route('**/api/v1/system/platform/module-instances?**', route => {
    const current = Number(new URL(route.request().url()).searchParams.get('page'))
    return route.fulfill({ json: { page: current, page_size: 10, total: 20, data: [{
      id: current, module_name: 'monitor', instance_id: current === 1 ? 'one' : 'two',
      role: 'backend', status: 'up', node_id: '', process_metrics_declared: false
    }] } })
  })
  let release
  const held = new Promise(resolve => { release = resolve })
  let oldStarted = false
  await page.route('**/api/v1/monitor/platform/traces/samples?**', async route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    if (params.instance_ids === '1') { oldStarted = true; await held }
    await route.fulfill({ json: { start: params.start, end: params.end, queried_at: new Date().toISOString(),
      policy_version: 2, coverage: 'sampled', data: [{
        instance_id: Number(params.instance_ids), state: params.instance_ids === '1' ? 'has_samples' : 'not_found'
      }] } }).catch(() => {})
  })
  await page.goto('/traces')
  await expect.poll(() => oldStarted).toBe(true)
  await expect(page.getByTestId('trace-sample-state')).toHaveText('检查中')
  await page.getByLabel('第 2 页', { exact: true }).click()
  await expect(page.getByTestId('trace-instances')).toContainText('two')
  await expect(page.getByTestId('trace-sample-state')).toHaveText('本时段未发现')
  release()
  await expect(page.getByTestId('trace-sample-state')).toHaveText('本时段未发现')
})

for (const locale of ['zh-cn','en']) test(`Trace detail shows sampled partial evidence and actual edges (${locale})`, async ({ page }, info) => {
  await backend(page, { locale }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page.getByTestId('trace-downstream')).toContainText('GET /system/check')
  await expect(page.getByTestId('trace-failure')).toHaveCount(0)
  await expect(page.getByTestId('trace-edges')).toContainText('system')
  await expect(page.getByTestId('trace-edges').locator('li')).toHaveCount(1)
  await expect(page.getByRole('button', { name: locale === 'en' ? 'Related logs' : '关联日志', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('traces')).not.toContainText('monitor.traces.')
  await page.screenshot({ path: info.outputPath(`traces-${locale}.png`), fullPage: true })
})
test('a failed call is reachable from the summary with its logs at narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 620, height: 760 })
  await backend(page, { identity: identity([...keys, 'platform.module_log.read']), failure: true })
  await page.goto('/traces?' + query())
  await expect(page.getByRole('heading', { name: 'GET /items/:id' })).toBeVisible()
  await expect(page.getByTestId('trace-instances')).toHaveCount(0)
  const failed = page.getByTestId('trace-spans').locator('li').filter({ hasText: '/system/check' })
  const summary = page.getByTestId('trace-failure')
  await expect(summary).toContainText('system')
  await expect(summary).toContainText('GET /system/check')
  await expect(summary).toContainText('HTTP 503')
  await expect(summary).toContainText('观测到的最长下游调用')
  await expect(page.getByTestId('trace-downstream')).toHaveCount(0)
  await expect(page.getByText('观测到 3 个失败 Span')).toBeVisible()
  await expect(summary.getByRole('button', { name: '关联日志', exact: true })).toBeInViewport()
  await summary.getByRole('button', { name: '定位到时间线', exact: true }).click()
  await expect(failed).toBeFocused()
  await expect(failed).toContainText('HTTP 503')
  await expect(failed.getByRole('button', { name: '关联日志', exact: true })).toBeInViewport()
  await failed.getByText('技术标识', { exact: true }).click()
  await expect(failed.getByText('4'.repeat(16), { exact: true })).toBeVisible()
  await expect.poll(() => page.getByTestId('trace-spans').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
})
test('the summary keeps separate failure and duration highlights when they point to different spans', async ({ page }) => {
  await backend(page, { failure: 'entry' })
  await page.goto('/traces?' + query())
  const failed = page.getByTestId('trace-failure')
  await expect(failed).toContainText('GET /items/:id')
  await expect(failed).toContainText('HTTP 503')
  await expect(failed.getByRole('button', { name: '关联日志', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('trace-downstream')).toContainText('GET /system/check')
  await expect(page.getByTestId('trace-downstream')).toContainText('HTTP 200')
})
test('Trace failure clears prior data and invalid projections cannot retain it', async ({ page }) => {
  const state = await backend(page); await page.goto('/traces?'+query()); await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  state.mode('foreign'); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect(page.getByTestId('trace-error')).toBeVisible(); await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  state.mode('failed'); await page.getByRole('button', { name: '刷新', exact: true }).click(); await expect(page.getByTestId('trace-spans')).toHaveCount(0)
})
for (const locale of ['zh-cn','en']) test(`Unavailable Trace service retains only the newly authorized instance list (${locale})`, async ({ page }) => {
  const state = await backend(page, { locale }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  state.mode('failed'); await page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  await expect(page.getByTestId('trace-error')).toContainText(locale === 'en' ? 'Trace service is unavailable' : '调用链服务不可用')
  await expect(page.getByTestId('trace-instances').locator('tbody tr')).toHaveCount(1)
  await expect(page.getByTestId('trace-instances')).toContainText('one')
  await expect(page.getByTestId('trace-results')).toContainText(locale === 'en' ? 'Query failed; results are unavailable' : '查询未成功，暂不显示结果')
  await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  await expect(page.getByTestId('trace-links')).toHaveCount(0)
  await expect(page.getByTestId('traces')).not.toContainText(locale === 'en' ? 'No matching instances' : '没有匹配的实例')
  state.mode('denied'); await page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  await expect(page.getByTestId('trace-error')).not.toContainText(locale === 'en' ? 'Trace service is unavailable' : '调用链服务不可用')
  await expect(page.getByTestId('trace-instances')).not.toContainText('one')
  await expect(page.getByTestId('trace-results')).toHaveCount(0)
})
test('Instance list failure is distinct from a successful empty list', async ({ page }) => {
  const state = await backend(page); state.mode('list_failed'); await page.goto('/traces')
  await expect(page.getByTestId('trace-error')).toBeVisible()
  await expect(page.getByTestId('trace-instances')).toContainText('查询未成功，暂不显示结果')
  await expect(page.getByTestId('traces')).not.toContainText('没有匹配的实例')
  expect(state.calls).toEqual([])
})
test('A pending Trace response cannot restore data after current User revocation', async ({ page }) => {
  const state = await backend(page); state.hold(); await page.goto('/traces?'+query()); await expect.poll(() => state.calls.length).toBe(1)
  await expect(page.getByTestId('trace-results')).not.toContainText('当前条件下没有可读的已保留调用链')
  await setIdentity(page, identity([])); state.release(); await expect(page.getByTestId('trace-spans')).toHaveCount(0); await expect(page.getByTestId('traces')).not.toContainText('/items/:id')
})
for (const [name, context, principal] of [['tenant',{type:'tenant',tenant_id:1},'user'],['machine',{type:'platform'},'service_principal']]) test(`Trace page denies ${name} before reading telemetry`, async ({ page }) => {
  const state = await backend(page,{identity:identity(keys,context,principal)});await page.goto('/traces?'+query());await expect(page.getByTestId('trace-spans')).toHaveCount(0);expect(state.calls).toEqual([])
})

for (const locale of ['zh-cn','en']) test(`Execution phase links open an independently authorized target window (${locale})`, async ({ page }, info) => {
  const state = await backend(page, { locale, links: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-spans')).toContainText(locale === 'en' ? 'Terminal' : '终态')
  await expect(page.getByTestId('trace-spans')).toContainText(locale === 'en' ? 'Failed' : '失败')
  await expect(page.getByTestId('trace-links').locator('li')).toHaveCount(1)
  await expect(page.getByTestId('trace-downstream')).toHaveCount(0)
  await expect(page.getByTestId('trace-edges')).toContainText(locale === 'en' ? 'No matched calls' : '没有已匹配的调用关系')
  await page.screenshot({ path: info.outputPath(`execution-links-${locale}.png`), fullPage: true })
  await page.getByRole('button', { name: locale === 'en' ? 'Follow link' : '查看关联', exact: true }).click()
  await expect.poll(() => state.calls.length).toBe(2)
  const target = state.calls[1]
  expect(target.instance_id).toBe('2')
  expect(Date.parse(target.start)).toBe(Date.parse(state.targetStart)-1000)
  expect(Date.parse(target.end)).toBe(Date.parse(state.targetStart)+1000)
  await expect(page.getByTestId('trace-spans')).toContainText('/items/:id')
  await expect(page).toHaveURL(new RegExp('trace_id='+'a'.repeat(32)))
})
test('Link target denial clears the source detail and all causal links', async ({ page }) => {
  await backend(page, { links: true, targetDenied: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-links')).toContainText('system')
  await page.getByRole('button', { name: '查看关联', exact: true }).click()
  await expect(page.getByTestId('trace-error')).toBeVisible()
  await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  await expect(page.getByTestId('trace-links')).toHaveCount(0)
})
test('Current User revocation clears execution stages and causal links', async ({ page }) => {
  await backend(page, { links: true }); await page.goto('/traces?'+query())
  await expect(page.getByTestId('trace-links')).toContainText('system')
  await setIdentity(page, identity([]))
  await expect(page.getByTestId('trace-spans')).toHaveCount(0)
  await expect(page.getByTestId('trace-links')).toHaveCount(0)
})
