import { test, expect } from '@playwright/test'

const createPermissions = ['transfer.task.create', 'meta.catalog.read']
const task = id => ({
  id, name: `navigation-task-${id}`, task_type: 'sync', enabled: false, schedule: '', status: 'idle',
  config: {
    runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' },
    source: { locator: 'addp://engine/9000001/path/synthetic/source?type=table', data_type: 'table', representation: 'native' },
    target: { parent_locator: 'addp://engine/9000002/path/synthetic?type=schema', name: 'target', data_type: 'table', representation: 'native', policy: { apply_mode: 'append' } },
    transforms: []
  }
})

async function setup(page, permissions, { saveFails = false, startFails = false, locale = 'zh-cn', editTask, metadata = {}, writes = [] } = {}) {
  await page.addInitScript(({ permissions, locale }) => {
    localStorage.setItem('fixture-transfer-permissions', JSON.stringify(permissions))
    localStorage.setItem('addp-lang', locale)
  }, { permissions, locale })
  await page.route(/\/(?:transfer\/)?(?:tasks(?:\/.*)?|forbidden)$/, async route => {
    if (!route.request().isNavigationRequest()) return route.continue()
    const fixture = await route.fetch({ url: new URL('/e2e/fixtures/task-navigation.html', route.request().url()).href })
    return route.fulfill({ response: fixture })
  })
  const calls = []
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    calls.push(`${method} ${path}`)
    if (method === 'GET' && Object.hasOwn(metadata, path)) return route.fulfill({ json: typeof metadata[path] === 'function' ? metadata[path](new URL(request.url())) : metadata[path] })
    if (path === '/api/v1/meta/engines') return route.fulfill({ json: [] })
    if (path === '/api/v1/transfer/capabilities') return route.fulfill({ json: {} })
    if (path === '/api/v1/transfer/task-definitions/statistics') return route.fulfill({ json: {} })
    if (method === 'POST' || method === 'PUT') {
      writes.push({ method, path, body: request.postDataJSON() })
      if (saveFails || (startFails && path.endsWith('/start'))) {
        return route.fulfill({ status: 409, json: { error: 'synthetic-save-or-start-failure' } })
      }
      if (path.endsWith('/start')) return route.fulfill({ json: {} })
      return route.fulfill({ json: task(method === 'POST' ? 42 : 17) })
    }
    if (/\/task-definitions\/\d+$/.test(path)) return route.fulfill({ json: editTask || task(Number(path.split('/').at(-1))) })
    if (/\/task-definitions(?:\/\d+\/executions)?$/.test(path)) return route.fulfill({ json: { items: [], data: [], total: 0 } })
    return route.abort('blockedbyclient')
  })
  return calls
}

async function review(surface, id = 42) {
  const wizard = surface.locator('.task-wizard')
  await expect(wizard).toBeVisible()
  // Seed only the form; submission, permissions, API calls and navigation are production code.
  await wizard.evaluate((element, draft) => {
    const state = element.__vueParentComponent.setupState.wizardState
    state.loadFromTask(draft)
    state.currentStep.value = 4
  }, task(id))
  await expect(surface.getByTestId('task-review-step')).toContainText(`navigation-task-${id}`)
}

async function confirm(surface, action = 'submit') {
  if (action === 'submit') await surface.getByTestId('task-submit').click()
  else await surface.getByTestId('task-review-step').getByRole('button', { name: '取消', exact: true }).click()
  await surface.locator('.el-message-box').getByRole('button', { name: '确定', exact: true }).click()
}

for (const mode of ['standalone', 'iframe']) {
  const publicPath = path => `${mode === 'iframe' ? '/transfer' : ''}${path}`
  const surfaceFor = page => mode === 'iframe' ? page.frameLocator('iframe') : page

  for (const locale of ['zh-cn', 'en']) {
    for (const canExecute of [false, true]) {
      test(`${mode}: ${locale} unscheduled creation feedback matches execute permission ${canExecute}`, async ({ page }) => {
        const calls = await setup(page, [...createPermissions, ...(canExecute ? ['transfer.task.execute'] : [])], { locale })
        await page.goto(publicPath('/tasks/create'))
        const surface = surfaceFor(page)
        await review(surface)
        await surface.getByTestId('task-review-step').getByRole('button', { name: locale === 'en' ? 'Previous' : '上一步', exact: true }).click()
        const label = canExecute
          ? (locale === 'en' ? 'Run Once Immediately' : '立即执行一次')
          : (locale === 'en' ? 'Save Task Only' : '仅保存任务')
        await expect(surface.getByRole('radio', { name: label, exact: true })).toBeChecked()
        if (!canExecute) await expect(surface.getByRole('radio', { name: locale === 'en' ? 'Run Once Immediately' : '立即执行一次', exact: true })).toHaveCount(0)
        await surface.getByTestId('task-wizard-next').click()
        const reviewStep = surface.getByTestId('task-review-step')
        await expect(reviewStep.getByTestId('task-review-schedule')).toHaveText(label)
        await expect(reviewStep.getByTestId('task-review-warnings')).toContainText(canExecute
          ? (locale === 'en' ? 'task will execute once immediately' : '任务将立即执行一次')
          : (locale === 'en' ? 'will only save the task and will not execute it automatically' : '本次仅保存任务，不会自动执行'))
        await surface.getByTestId('task-submit').click()
        await surface.locator('.el-message-box').getByRole('button', { name: locale === 'en' ? 'OK' : '确定', exact: true }).click()
        await expect(surface.locator('.task-wizard')).toHaveAttribute('data-step', '0')
        expect(calls.filter(call => call.startsWith('POST '))).toEqual([
          'POST /api/v1/transfer/task-definitions',
          ...(canExecute ? ['POST /api/v1/transfer/task-definitions/42/start'] : [])
        ])
        expect(calls.filter(call => call.startsWith('GET /api/v1/transfer/task-definitions'))).toEqual([])
      })
    }
  }

  test(`${mode}: create uses returned ID, replace history and recoverable detail without update permission`, async ({ page }) => {
    const calls = await setup(page, [...createPermissions, 'transfer.task.read'])
    await page.goto(publicPath('/tasks'))
    const surface = surfaceFor(page)
    await surface.getByRole('button', { name: '创建任务', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/create')}$`))
    await review(surface, 99)
    const frame = mode === 'iframe' ? page.frames().find(frame => frame.parentFrame()) : page.mainFrame()
    const instance = await frame.evaluate(() => window.fixtureInstance)
    const historyLength = await page.evaluate(() => history.length)
    await confirm(surface)
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/42/detail')}$`))
    await expect(surface.locator('.task-detail')).toContainText('navigation-task-42')
    expect(await page.evaluate(() => history.length)).toBe(historyLength)
    expect(await frame.evaluate(() => window.fixtureInstance)).toBe(instance)
    if (mode === 'iframe') {
      const navigations = await page.evaluate(() => window.fixtureNavigations)
      expect(navigations.map(({ route, history }) => [route, history])).toEqual([
        ['/transfer/tasks/create', 'push'], ['/transfer/tasks/42/detail', 'replace']
      ])
      expect(await frame.evaluate(() => location.pathname)).toBe('/tasks/42/detail')
      await expect(page.locator('iframe')).toHaveCount(1)
    }
    await page.goBack()
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks')}$`))
    await page.goForward()
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/42/detail')}$`))
    await expect(surface.locator('.task-detail')).toContainText('navigation-task-42')
    await page.reload()
    await expect(surface.locator('.task-detail')).toContainText('navigation-task-42')
    expect(calls.filter(call => call.startsWith('POST '))).toEqual(['POST /api/v1/transfer/task-definitions'])
    expect(calls.filter(call => call.startsWith('PUT '))).toEqual([])
  })

  test(`${mode}: scheduled creation keeps its cron summary and never starts immediately`, async ({ page }) => {
    const calls = await setup(page, [...createPermissions, 'transfer.task.execute', 'system.execution_authorization.create'])
    await page.goto(publicPath('/tasks/create'))
    const surface = surfaceFor(page)
    await review(surface)
    await surface.locator('.task-wizard').evaluate(element => {
      element.__vueParentComponent.setupState.wizardState.schedule.value = '0 0 0 1 * *'
    })
    await expect(surface.getByTestId('task-review-schedule')).toHaveText('0 0 0 1 * *')
    await expect(surface.getByTestId('task-review-warnings')).not.toContainText('任务将立即执行一次')
    await expect(surface.getByTestId('task-review-warnings')).not.toContainText('本次仅保存任务')
    await confirm(surface)
    await expect(surface.locator('.task-wizard')).toHaveAttribute('data-step', '0')
    expect(calls.filter(call => call.startsWith('POST '))).toEqual(['POST /api/v1/transfer/task-definitions'])
  })

  test(`${mode}: create-only success resets, refresh stays empty and forbidden deep links never read`, async ({ page }) => {
    const calls = await setup(page, createPermissions)
    await page.goto(publicPath('/tasks/create'))
    const surface = surfaceFor(page)
    await review(surface)
    const historyLength = await page.evaluate(() => history.length)
    await confirm(surface)
    await expect(surface.locator('.task-wizard')).toHaveAttribute('data-step', '0')
    await expect(surface.locator('.el-message--success')).toBeVisible()
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/create')}$`))
    expect(await page.evaluate(() => history.length)).toBe(historyLength)
    await page.reload()
    await expect(surface.locator('.task-wizard')).toHaveAttribute('data-step', '0')
    expect(calls.filter(call => call.startsWith('GET /api/v1/transfer/task-definitions'))).toEqual([])
    expect(calls.filter(call => call.startsWith('POST '))).toHaveLength(1)
    // Module guard is also enforced for direct object and list URLs.
    for (const path of ['/tasks/42/detail', '/tasks/42/edit', '/tasks']) {
      await page.goto(publicPath(path))
      await expect(surface.locator('.task-wizard, .task-detail, .task-list')).toHaveCount(0)
      const frame = mode === 'iframe' ? page.frames().find(frame => frame.parentFrame()) : page.mainFrame()
      await expect.poll(() => frame.evaluate(() => location.pathname)).toBe('/forbidden')
    }
    expect(calls.filter(call => call.startsWith('GET /api/v1/transfer/task-definitions'))).toEqual([])
  })

  for (const edit of [false, true]) {
    test(`${mode}: ${edit ? 'edit' : 'create'} save failure keeps form and URL`, async ({ page }) => {
      const calls = await setup(page, [...createPermissions, 'transfer.task.read', 'transfer.task.update'], { saveFails: true })
      const path = publicPath(edit ? '/tasks/17/edit' : '/tasks/create')
      await page.goto(path)
      const surface = surfaceFor(page)
      await review(surface, 17)
      await confirm(surface)
      await expect(surface.locator('.el-message--error').filter({ hasText: 'synthetic-save-or-start-failure' }).first()).toBeVisible()
      await expect(page).toHaveURL(new RegExp(`${path}$`))
      await expect(surface.getByTestId('task-review-step')).toContainText('navigation-task-17')
      expect(calls.filter(call => call.startsWith(edit ? 'PUT ' : 'POST '))).toHaveLength(1)
    })
  }

  test(`${mode}: edit save and readable cancel replace the form with the list`, async ({ page }) => {
    const calls = await setup(page, [...createPermissions, 'transfer.task.read', 'transfer.task.update', 'transfer.task.execute'])
    const surface = surfaceFor(page)
    for (const action of ['submit', 'cancel']) {
      await page.goto(publicPath('/tasks/17/edit'))
      await review(surface, 17)
      await surface.getByTestId('task-review-step').getByRole('button', { name: '上一步', exact: true }).click()
      await expect(surface.getByRole('radio', { name: '仅保存任务', exact: true })).toBeChecked()
      await surface.getByTestId('task-wizard-next').click()
      await expect(surface.getByTestId('task-review-schedule')).toHaveText('仅保存任务')
      await expect(surface.getByTestId('task-review-warnings')).toContainText('本次仅保存任务，不会自动执行')
      const historyLength = await page.evaluate(() => history.length)
      await confirm(surface, action)
      await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks')}$`))
      expect(await page.evaluate(() => history.length)).toBe(historyLength)
    }
    expect(calls.filter(call => call.startsWith('PUT '))).toEqual(['PUT /api/v1/transfer/task-definitions/17'])
    expect(calls.filter(call => call.startsWith('POST '))).toEqual([])
  })

  test(`${mode}: create-only cancel resets; dismissing submit preserves the draft`, async ({ page }) => {
    const calls = await setup(page, createPermissions)
    await page.goto(publicPath('/tasks/create'))
    const surface = surfaceFor(page)
    await review(surface)
    await surface.getByTestId('task-submit').click()
    await surface.locator('.el-message-box').getByRole('button', { name: '取消', exact: true }).click()
    await expect(surface.getByTestId('task-review-step')).toContainText('navigation-task-42')
    await confirm(surface, 'cancel')
    await expect(surface.locator('.task-wizard')).toHaveAttribute('data-step', '0')
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/create')}$`))
    expect(calls.filter(call => call.startsWith('POST ') || call.startsWith('PUT ') || call.startsWith('GET /api/v1/transfer/task-definitions'))).toEqual([])
  })

  test(`${mode}: start failure after saved creation still lands on the created detail`, async ({ page }) => {
    const calls = await setup(page, [...createPermissions, 'transfer.task.read', 'transfer.task.execute'], { startFails: true })
    await page.goto(publicPath('/tasks/create'))
    const surface = surfaceFor(page)
    await review(surface)
    await confirm(surface)
    await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks/42/detail')}$`))
    await expect(surface.locator('.el-message--warning')).toContainText('synthetic-save-or-start-failure')
    expect(calls.filter(call => call.startsWith('POST '))).toEqual([
      'POST /api/v1/transfer/task-definitions', 'POST /api/v1/transfer/task-definitions/42/start'
    ])
  })
}

// The API fixtures are synthetic; loading, form state, PUT and navigation use the production editor.
function editDraft(autoScan, configBatchSize) {
  const fields = Array.from({ length: 16 }, (_, i) => `field_${i}`)
  const draft = {
    ...task(17), description: 'synthetic edit fidelity', batch_size: 1000,
    auto_scan_metadata: autoScan,
    config: {
      runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' },
      source: {
        locator: 'addp://engine/9000001/path/synthetic/records?type=collection&item_id=90000199',
        data_type: 'table', representation: 'native',
        query: { language: 'mql', statement: JSON.stringify({ aggregate: 'records', pipeline: [{ $project: Object.fromEntries(fields.map(name => [name, `$${name}`])) }] }) }
      },
      target: { parent_locator: 'addp://engine/9000002/path/synthetic?type=schema', name: 'edit_target', data_type: 'table', representation: 'native', policy: { apply_mode: 'replace' } },
      transforms: [{ type: 'field_mapping', version: 'v1', mode: 'project', fields: fields.map(name => ({ source: name, target: name, target_type: 'json', nullable: true })) }],
      ...(configBatchSize === undefined ? {} : { batch_size: configBatchSize })
    }
  }
  return draft
}

function editMetadata() {
  const fields = Array.from({ length: 16 }, (_, i) => ({ name: `field_${i}`, type: 'json', nullable: true }))
  return {
    '/api/v1/meta/engines': [
      { id: 9000001, engine_type: 'mongodb', name: 'Synthetic source' },
      { id: 9000002, engine_type: 'postgresql', name: 'Synthetic target' }
    ],
    '/api/v1/meta/items/90000199': { id: 90000199, name: 'records', full_name: 'synthetic.records', item_type: 'collection', attributes: { item: { data_type: 'table', representation: 'native' } } },
    '/api/v1/meta/items/90000199/fields': { data: fields }
  }
}

async function currentPayload(surface) {
  return surface.locator('.task-wizard').evaluate(element => JSON.parse(JSON.stringify(element.__vueParentComponent.setupState.wizardState.taskConfig.value)))
}

function requestFromDraft(draft) {
  const { name, description, task_type, config, schedule, enabled, batch_size, auto_scan_metadata } = draft
  return structuredClone({ name, description, task_type, config, schedule, enabled, batch_size, auto_scan_metadata })
}

for (const mode of ['standalone', 'iframe']) {
  const surfaceFor = page => mode === 'iframe' ? page.frameLocator('iframe') : page
  const publicPath = path => `${mode === 'iframe' ? '/transfer' : ''}${path}`
  for (const autoScan of [false, true]) {
    for (const override of [undefined, 2000]) {
      test(`${mode}: edit preserves scan=${autoScan} and batch override=${override ?? 'absent'} through real load and one batch-only save`, async ({ page }) => {
        const draft = editDraft(autoScan, override)
        const before = structuredClone(draft)
        const writes = []
        const calls = await setup(page, [...createPermissions, 'transfer.task.read', 'transfer.task.update', 'transfer.task.execute'], { editTask: draft, metadata: editMetadata(), writes })
        await page.goto(publicPath('/tasks/17/edit'))
        const surface = surfaceFor(page)
        await expect(surface.locator('.el-message--success').filter({ hasText: '任务详情加载成功' }).first()).toBeVisible()
        const expected = requestFromDraft(draft)
        // No injected draft: GET -> loadFromTask -> metadata restoration -> taskConfig.
        expect(await currentPayload(surface)).toEqual(expected)
        expect(draft).toEqual(before)
        // Move to the configure step without changing any business form values.
        await surface.locator('.task-wizard').evaluate(element => { element.__vueParentComponent.setupState.wizardState.currentStep.value = 3 })
        const batch = surface.getByRole('spinbutton', { name: '批处理大小', exact: true })
        await expect(batch).toHaveValue(String(override ?? 1000))
        await batch.fill('500')
        await batch.press('Tab')
        await surface.getByTestId('task-wizard-next').click()
        if (override === undefined) expected.batch_size = 500
        else expected.config.batch_size = 500
        expect(await currentPayload(surface)).toEqual(expected)
        await confirm(surface)
        await expect(page).toHaveURL(new RegExp(`${publicPath('/tasks')}$`))
        expect(writes).toEqual([{ method: 'PUT', path: '/api/v1/transfer/task-definitions/17', body: expected }])
        expect(calls.filter(call => call.startsWith('POST '))).toEqual([])
        expect(calls.some(call => /start|scan|run-approvals/.test(call))).toBe(false)
      })
    }
  }
}

test('new wizard and reset retain the default scan flag for bounded and continuous tasks', async ({ page }) => {
  await setup(page, createPermissions)
  await page.goto('/tasks/create')
  const wizard = page.locator('.task-wizard')
  await expect(wizard).toBeVisible()
  const result = await wizard.evaluate(element => {
    const state = element.__vueParentComponent.setupState.wizardState
    const result = { newBounded: state.taskConfig.value.auto_scan_metadata, newConfigHasBatch: Object.hasOwn(state.taskConfig.value.config, 'batch_size') }
    state.runtimeBoundary.value = 'continuous'
    result.newContinuous = state.taskConfig.value.auto_scan_metadata
    state.loadFromTask({ name: 'temporary edit', auto_scan_metadata: false, batch_size: 321, config: { batch_size: 123 } })
    result.loadedFalse = state.taskConfig.value.auto_scan_metadata
    state.reset()
    result.resetBounded = state.taskConfig.value.auto_scan_metadata
    result.resetBatch = state.taskConfig.value.batch_size
    result.resetConfigHasBatch = Object.hasOwn(state.taskConfig.value.config, 'batch_size')
    state.runtimeBoundary.value = 'continuous'
    result.resetContinuous = state.taskConfig.value.auto_scan_metadata
    return result
  })
  expect(result).toEqual({ newBounded: true, newConfigHasBatch: false, newContinuous: true, loadedFalse: false, resetBounded: true, resetBatch: 1000, resetConfigHasBatch: false, resetContinuous: true })
})
