import { expect, test } from '@playwright/test'

const hostURL = '/e2e/fixtures/leave-guard.html?editor=service-navigation'
const dialog = owner => owner.getByRole('dialog', { name: '有未保存的修改' })
const savedService = {
  id: 49, service_name: 'registered_fixture', title: 'Registered fixture',
  description: 'Saved description', keywords: ['saved'], service_type: 'rest',
  endpoint_url: 'https://example.invalid/registered', auth_type: 'none', status: 'active'
}

async function openForm(page, { edit = false, auth = 'none' } = {}) {
  const writes = []
  let service = { ...savedService, auth_type: auth }
  let saveStatus = 200
  let pendingSave
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => { throw error })
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET') {
      if (path === '/api/v1/service/registered/49') return route.fulfill({ json: service })
      if (path === '/api/v1/service/registered') return route.fulfill({ json: { data: [service], total: 1, page: 1, limit: 20 } })
      if (path === '/api/v1/service/query') return route.fulfill({ json: { data: [], total: 0 } })
    }
    if ((request.method() === 'POST' && path === '/api/v1/service/registered') ||
        (request.method() === 'PUT' && path === '/api/v1/service/registered/49')) {
      const body = request.postDataJSON()
      writes.push({ method: request.method(), body })
      if (pendingSave) await pendingSave
      if (saveStatus === 200) service = { ...service, ...body }
      return route.fulfill({ status: saveStatus, json: saveStatus === 200 ? service : { error: 'fixture registration save failed' } })
    }
    await route.abort()
    throw new Error(`Unexpected API request: ${request.method()} ${path}`)
  })
  await page.goto(`${hostURL}#/service/services`)
  const editor = page.frameLocator('iframe')
  await expect(editor.getByRole('heading', { name: '注册服务', exact: true })).toBeVisible()
  await editor.getByRole('button', { name: edit ? '编辑' : '+ 注册外部服务', exact: true }).click()
  await expect(editor.getByRole('heading', { name: edit ? '编辑注册服务' : '注册外部服务', exact: true })).toBeVisible()
  if (edit) await expect(editor.locator('#title')).toHaveValue(savedService.title)
  return {
    editor, writes,
    failSaving: () => { saveStatus = 500 },
    allowSaving: () => { saveStatus = 200 },
    holdSaving: () => {
      let release
      pendingSave = new Promise(resolve => { release = resolve })
      return release
    }
  }
}

async function enterDraft(editor) {
  await editor.locator('#service_name').fill('registered_draft')
  await editor.locator('#title').fill('Registered draft')
  await editor.locator('#description').fill('Unsaved description')
  await editor.locator('#keywords').fill('alpha, beta')
  await editor.locator('#service_type').selectOption('rest')
  await editor.locator('#endpoint_url').fill('https://example.invalid/draft')
  await editor.locator('#auth_type').selectOption('bearer')
  await editor.locator('#token').fill('fixture-only-token')
}

async function expectDraft(page, editor) {
  await expect(page).toHaveURL(`${hostURL}#/service/services/create`)
  await expect(editor.locator('#title')).toHaveValue('Registered draft')
  await expect(editor.locator('#description')).toHaveValue('Unsaved description')
  await expect(editor.locator('#keywords')).toHaveValue('alpha, beta')
  await expect(editor.locator('#token')).toHaveValue('fixture-only-token')
}

async function cancelLeave(owner) {
  await expect(dialog(owner)).toBeVisible()
  await dialog(owner).getByRole('button', { name: '继续编辑', exact: true }).click()
  await expect(dialog(owner)).toHaveCount(0)
}

async function expectList(page, editor) {
  await expect(page).toHaveURL(`${hostURL}#/service/services`)
  await expect(editor.getByRole('heading', { name: '注册服务', exact: true })).toBeVisible()
}

test('registered form internal cancel preserves fields, keywords and credentials; discard navigates once', async ({ page }) => {
  const { editor, writes } = await openForm(page)
  await enterDraft(editor)
  await editor.locator('body').evaluate(element => element.dataset.registrationDocument = 'same')
  const historyLength = await page.evaluate(() => history.length)
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog(page)).toHaveCount(0)
  await cancelLeave(editor)
  await expectDraft(page, editor)
  await editor.getByRole('button', { name: '← 返回', exact: true }).click()
  await dialog(editor).getByRole('button', { name: '放弃修改并离开', exact: true }).click()
  await expectList(page, editor)
  await expect(editor.locator('body')).toHaveAttribute('data-registration-document', 'same')
  expect(await page.evaluate(() => history.length)).toBe(historyLength)
  await expect(dialog(page)).toHaveCount(0)
  expect(writes).toEqual([])
})

test('registered draft protects Console menu and history; forward after discard opens a clean form', async ({ page }) => {
  const { editor, writes } = await openForm(page)
  await enterDraft(editor)
  await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
  await expect(dialog(editor)).toHaveCount(0)
  await cancelLeave(page)
  await expectDraft(page, editor)
  await page.evaluate(() => history.back())
  await cancelLeave(page)
  await expectDraft(page, editor)
  await page.evaluate(() => history.back())
  await dialog(page).getByRole('button', { name: '放弃修改并离开', exact: true }).click()
  await expectList(page, editor)
  await page.goForward()
  await expect(page).toHaveURL(`${hostURL}#/service/services/create`)
  await expect(editor.locator('#title')).toHaveValue('')
  await expect(editor.locator('#keywords')).toHaveValue('')
  await expect(editor.locator('#auth_type')).toHaveValue('none')
  await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
  await expect(page).toHaveURL(`${hostURL}#/service/query-services`)
  await expect(dialog(page)).toHaveCount(0)
  expect(writes).toEqual([])
})

test('registered draft cancels browser refresh and retains credential input', async ({ page }) => {
  const { editor, writes } = await openForm(page)
  await enterDraft(editor)
  const pendingDialog = page.waitForEvent('dialog')
  await page.evaluate(() => { setTimeout(() => location.reload(), 0) })
  const unload = await pendingDialog
  expect(unload.type()).toBe('beforeunload')
  await unload.dismiss()
  await expectDraft(page, editor)
  expect(writes).toEqual([])
})

for (const edit of [false, true]) {
  test(`pristine registered ${edit ? 'edit' : 'create'} form leaves without confirmation`, async ({ page }) => {
    const { writes } = await openForm(page, { edit })
    await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
    await expect(page).toHaveURL(`${hostURL}#/service/query-services`)
    await expect(dialog(page)).toHaveCount(0)
    expect(writes).toEqual([])
  })
}

for (const [auth, field] of [['basic', '#password'], ['bearer', '#token'], ['api_key', '#api_key']]) {
  test(`changing only ${auth} credentials marks the loaded registration dirty`, async ({ page }) => {
    const { editor, writes } = await openForm(page, { edit: true, auth })
    await expect(editor.locator(field)).toHaveValue('')
    await editor.locator(field).fill('fixture-only-credential')
    await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
    await cancelLeave(page)
    await expect(page).toHaveURL(`${hostURL}#/service/services/49/edit`)
    await expect(editor.locator(field)).toHaveValue('fixture-only-credential')
    await expect(editor.locator('#title')).toHaveValue(savedService.title)
    expect(writes).toEqual([])
  })
}

for (const edit of [false, true]) {
  test(`registered ${edit ? 'update' : 'creation'} retains failed input and clears protection after saving`, async ({ page }) => {
    const h = await openForm(page, { edit })
    if (edit) {
      await h.editor.locator('#title').fill('Registered draft')
      await h.editor.locator('#keywords').fill('alpha, beta')
      await h.editor.locator('#auth_type').selectOption('bearer')
      await h.editor.locator('#token').fill('fixture-only-token')
    } else await enterDraft(h.editor)
    const save = h.editor.getByRole('button', { name: edit ? '保存修改' : '创建服务', exact: true })
    h.failSaving()
    await save.click()
    await expect.poll(() => h.writes.length).toBe(1)
    await expect(h.editor.locator('.el-message--error')).toContainText('保存失败')
    await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
    await cancelLeave(page)
    await expect(h.editor.locator('#title')).toHaveValue('Registered draft')
    await expect(h.editor.locator('#keywords')).toHaveValue('alpha, beta')
    await expect(h.editor.locator('#token')).toHaveValue('fixture-only-token')
    h.allowSaving()
    const release = h.holdSaving()
    const historyLength = await page.evaluate(() => history.length)
    try {
      await save.click()
      await expect.poll(() => h.writes.length).toBe(2)
      await expect(h.editor.locator('#title')).toBeDisabled()
      await expect(h.editor.locator('#token')).toBeDisabled()
    } finally { release() }
    await expect(page).toHaveURL(`${hostURL}#/service/services/49`)
    await expect(h.editor.getByRole('heading', { name: 'Registered draft', exact: true })).toBeVisible()
    await expect(dialog(page)).toHaveCount(0)
    await expect(dialog(h.editor)).toHaveCount(0)
    expect(await page.evaluate(() => history.length)).toBe(historyLength)
    expect(h.writes).toHaveLength(2)
    expect(h.writes[1]).toMatchObject({ method: edit ? 'PUT' : 'POST', body: {
      title: 'Registered draft', keywords: ['alpha', 'beta'], auth_type: 'bearer', auth_config: { token: 'fixture-only-token' }
    } })
    page.on('dialog', () => { throw new Error('Saved registration must not block refresh') })
    await page.reload()
    await expect(h.editor.getByRole('heading', { name: 'Registered draft', exact: true })).toBeVisible()
    await page.goBack()
    await expectList(page, h.editor)
  })
}

for (const edit of [false, true]) {
  for (const outcome of ['success', 'failure']) {
    test(`late registered ${edit ? 'update' : 'creation'} ${outcome} after confirmed leave cannot disturb a new draft`, async ({ page }) => {
      const h = await openForm(page, { edit })
      if (edit) await h.editor.locator('#title').fill('Submitted draft')
      else await enterDraft(h.editor)
      if (outcome === 'failure') h.failSaving()
      const release = h.holdSaving()
      try {
        await h.editor.locator('body').evaluate(element => element.dataset.registrationDocument = 'pending-save')
        await h.editor.getByRole('button', { name: edit ? '保存修改' : '创建服务', exact: true }).click()
        await expect.poll(() => h.writes.length).toBe(1)
        // The header back button remains available while form controls are disabled.
        await h.editor.getByRole('button', { name: '← 返回', exact: true }).click()
        await expect(dialog(h.editor)).toBeVisible()
        await expect(dialog(page)).toHaveCount(0)
        await dialog(h.editor).getByRole('button', { name: '放弃修改并离开', exact: true }).click()
        await expectList(page, h.editor)
        await h.editor.getByRole('button', { name: '+ 注册外部服务', exact: true }).click()
        await h.editor.locator('#title').fill('New draft while old save is pending')
        await h.editor.locator('#keywords').fill('new, unsaved')
        await expect(h.editor.locator('body')).toHaveAttribute('data-registration-document', 'pending-save')
        const response = page.waitForResponse(response => response.request().method() === (edit ? 'PUT' : 'POST') && new URL(response.url()).pathname === (edit ? '/api/v1/service/registered/49' : '/api/v1/service/registered'))
        release()
        await (await response).finished()
        await h.editor.locator('#title').evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
        await expect(page).toHaveURL(`${hostURL}#/service/services/create`)
        await expect(dialog(page)).toHaveCount(0)
        await expect(dialog(h.editor)).toHaveCount(0)
        await expect(h.editor.locator('.el-message')).toHaveCount(0)
        await expect(h.editor.locator('#title')).toHaveValue('New draft while old save is pending')
        await expect(h.editor.locator('#keywords')).toHaveValue('new, unsaved')
        await expect(h.editor.locator('#title')).toBeEnabled()
        await page.getByRole('menuitem', { name: '查询服务', exact: true }).click()
        await cancelLeave(page)
        await expect(h.editor.locator('#title')).toHaveValue('New draft while old save is pending')
        expect(h.writes).toHaveLength(1)
      } finally {
        release()
        await page.unrouteAll({ behavior: 'wait' })
      }
    })
  }
}
