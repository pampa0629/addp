import { expect, test } from '@playwright/test'

const runId = 'ace1d263-f309-4c67-8f47-761cfa691f35'
const plan = [{ content: '确认输入', status: 'completed' }, { content: '检查结果', status: 'pending' }]
const surface = 'table:workspace'
const resultMessage = { id: 71, protocol_message_id: 'result-71', role: 'assistant', parts: [
  { type: 'presentation_ref', protocol: 'ag-ui', activity_type: 'addp-plan', surface_id: 'plan:1', content: { agent_run_id: runId, todos: plan } },
  { type: 'presentation_ref', protocol: 'a2ui', surface_id: surface, content: { operations: [
    { version: 'v0.9', createSurface: { surfaceId: surface, catalogId: 'addp.catalog/v1' } },
    { version: 'v0.9', updateComponents: { surfaceId: surface, components: [{ id: 'root', component: 'TablePreview', columns: ['name'], rows: [{ name: '示例' }], total: 1, truncated: false }] } }
  ] } }
] }

async function installWorkspace(page, { conflict = false } = {}) {
  const session = { id: 61, title: '数据分析会话', version: 1, pinned: false, archived: false, created_at: '2026-10-29T00:00:00Z', updated_at: '2026-10-30T00:00:00Z' }
  const writes = [], queries = [], unexpected = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => { throw error })
  await page.route(/^https?:\/\/[^/]+\/api\//, async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let body
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = { context: { type: 'tenant', tenant_id: '7' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions: ['agent.session.read','agent.session.create','agent.session.update','agent.run.create','agent.run.execute','agent.run.read'] }] } }
    else if (path === '/api/v1/agent/skills') body = [{ name: 'query-generation', description: 'SQL 草稿' }]
    else if (path === '/api/v1/agent/sessions') {
      queries.push(Object.fromEntries(url.searchParams))
      const matches = String(session.archived) === url.searchParams.get('archived') && session.title.includes(url.searchParams.get('search') || '')
      body = { data: matches ? [session] : [], total: matches ? 1 : 0, page: 1, page_size: 20, total_pages: matches ? 1 : 0 }
    } else if (path === '/api/v1/agent/sessions/61' && request.method() === 'GET') body = session
    else if (path === '/api/v1/agent/sessions/61' && request.method() === 'PATCH') {
      const value = request.postDataJSON(); writes.push(value)
      if (conflict && writes.length === 1) {
        session.version++
        return route.fulfill({ status: 409, json: { error: '会话已变化', error_code: 'resource_version_conflict' } })
      }
      expect(value.version).toBe(session.version)
      Object.assign(session, value, { version: session.version + 1 }); body = session
    } else if (path === '/api/v1/agent/sessions/61/messages') body = [resultMessage]
    else if (path === '/api/v1/agent/sessions/61/runs') body = { data: [{ id: runId, skill_name: 'query-generation', status: 'waiting', created_at: '2026-10-30T00:00:00Z' }], total: 1, page: 1, page_size: 20, total_pages: 1 }
    else if (path === `/api/v1/agent/runs/${runId}`) body = { id: runId, status: 'waiting', plan, steps: [{ id: 'step-1', tool_name: 'resource.facts.get', step_type: 'tool_call', status: 'completed', started_at: '2026-10-30T00:00:00Z', completed_at: '2026-10-30T00:00:02Z', output_summary: '{"field_count":2}' }] }
    else { unexpected.push(`${request.method()} ${path}`); return route.fulfill({ status: 500, json: { error: 'Unexpected fixture request' } }) }
    return route.fulfill({ json: body })
  })
  return { writes, queries, unexpected }
}

async function menu(page, name) {
  await page.getByRole('button', { name: '会话操作', exact: true }).click()
  await page.getByRole('menuitem', { name, exact: true }).click()
}

test('capability home fills a draft without submitting, searches titles and keeps the home route', async ({ page }) => {
  const backend = await installWorkspace(page)
  await page.goto('/module-ui/agent/')
  await expect(page.getByRole('heading', { name: '你好！我是 ADDP AI 助手。' })).toBeVisible()
  await page.getByRole('button', { name: /编写查询/ }).click()
  await expect(page.locator('textarea')).toHaveValue(/只生成草稿/)
  expect(backend.writes).toEqual([])
  await page.getByPlaceholder('搜索会话标题').fill('不存在')
  await page.getByPlaceholder('搜索会话标题').press('Enter')
  await expect(page.getByText('暂无对话', { exact: true })).toBeVisible()
  expect(backend.queries.at(-1)).toMatchObject({ search: '不存在', page: '1', page_size: '20' })
  expect(backend.unexpected).toEqual([])
})

test('archiving keeps history readable across reload and requires explicit restore to send', async ({ page }) => {
  const backend = await installWorkspace(page)
  await page.goto('/module-ui/agent/sessions/61')
  await expect(page.locator('textarea')).toBeEnabled()
  await menu(page, '归档')
  await expect(page.getByText('此会话已归档，恢复后可以继续对话。已有运行继续。')).toBeVisible()
  await expect(page.locator('textarea')).toBeDisabled()
  await page.reload()
  await expect(page.locator('textarea')).toBeDisabled()
  await expect(page.getByText('示例', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '恢复会话', exact: true }).click()
  await expect(page.locator('textarea')).toBeEnabled()
  expect(backend.writes).toEqual([{ version: 1, archived: true }, { version: 2, archived: false }])
  expect(backend.unexpected).toEqual([])
})

test('rename conflict retains the draft and reloads a version only on request', async ({ page }) => {
  const backend = await installWorkspace(page, { conflict: true })
  await page.goto('/module-ui/agent/sessions/61')
  await menu(page, '重命名')
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox').fill('我的新标题')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox')).toHaveValue('我的新标题')
  await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(backend.writes).toHaveLength(1)
  await dialog.getByRole('button', { name: '重新加载版本', exact: true }).click()
  await expect(dialog.getByRole('textbox')).toHaveValue('我的新标题')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(backend.writes).toEqual([{ version: 1, title: '我的新标题' }, { version: 2, title: '我的新标题' }])
  expect(backend.unexpected).toEqual([])
})

test('historical process, plan and result source survive refresh in narrow and dark layouts', async ({ page }, testInfo) => {
  const backend = await installWorkspace(page)
  await page.goto('/module-ui/agent/sessions/61')
  await page.reload()
  await page.getByRole('button', { name: '过程与成果', exact: true }).click()
  const panel = page.getByRole('complementary', { name: '过程与成果' })
  await expect(panel.getByText('resource.facts.get', { exact: true })).toBeVisible()
  await expect(panel.getByText('2.0 s', { exact: true })).toBeVisible()
  await expect(panel.getByText('检查结果', { exact: true })).toBeVisible()
  await expect(panel.getByText('计划进度不代表业务执行或验证通过；请结合执行记录和正式报告判断。')).toBeVisible()
  await panel.getByRole('tab', { name: '成果 (1)' }).click()
  await panel.getByRole('button', { name: /表格/ }).click()
  await expect(page.locator('[data-message-id="result-71"]')).toBeFocused()
  await page.evaluate(() => document.documentElement.className = 'dark')
  await page.setViewportSize({ width: 520, height: 850 })
  await expect(panel.getByRole('button', { name: '关闭', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('agent-workspace-dark.png') })
  await panel.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(panel).not.toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
  expect(backend.unexpected).toEqual([])
})
