import { readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
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

async function installWorkspace(page, { conflict = false, configuration = false, history = [resultMessage], identity = { user: 9, tenant: '7' } } = {}) {
  const session = { id: 61, title: '数据分析会话', version: 1, pinned: false, archived: false, created_at: '2026-10-29T00:00:00Z', updated_at: '2026-10-30T00:00:00Z' }
  const writes = [], queries = [], unexpected = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => { throw error })
  await page.route(/^https?:\/\/[^/]+\/api\//, async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let body
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: identity.user, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = { context: { type: 'tenant', tenant_id: identity.tenant }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: identity.tenant }, permissions: ['agent.session.read','agent.session.create','agent.session.update','agent.run.create','agent.run.execute','agent.run.read', ...(configuration ? ['agent.configuration.read'] : [])] }] } }
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
    } else if (path === '/api/v1/agent/sessions/61/messages') body = history
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
  await page.setViewportSize({ width: 620, height: 900 })
  await page.getByRole('button', { name: '筛选会话', exact: true }).click()
  await page.getByRole('menuitem', { name: '已归档', exact: true }).click()
  const labelLines = await page.locator('.active-filter').evaluate(element => {
    const range = document.createRange()
    range.selectNodeContents(element)
    return new Set([...range.getClientRects()].map(rect => Math.round(rect.top))).size
  })
  expect(labelLines).toBe(1)
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

test('pending interaction stays readable but disabled while archived and reopens after restore', async ({ page }) => {
  const interactionId = '1b842c47-cdf4-4228-af6e-25bfbaa8609b'
  const history = [{ id: 72, role: 'assistant', parts: [
    { type: 'interaction_ref', interaction_id: interactionId, status: 'pending' },
    { type: 'presentation_ref', protocol: 'a2ui', interaction_id: interactionId, surface_id: 'waiting:1', content: { operations: [
      { version: 'v0.9', createSurface: { surfaceId: 'waiting:1', catalogId: 'addp.catalog/v1' } },
      { version: 'v0.9', updateComponents: { surfaceId: 'waiting:1', components: [{ id: 'root', component: 'ClarificationChoice', interactionId, prompt: '请选择引擎', options: [{ label: 'GeoPython', value: 6 }] }] } }
    ] } }
  ] }]
  const backend = await installWorkspace(page, { history })
  await page.goto('/module-ui/agent/sessions/61')
  const choice = page.getByRole('button', { name: 'GeoPython', exact: true })
  await expect(choice).toBeEnabled()
  await menu(page, '归档')
  await expect(choice).toBeDisabled()
  await expect(page.getByText('请选择引擎', { exact: true })).toBeVisible()
  await page.reload()
  await expect(choice).toBeDisabled()
  await page.getByRole('button', { name: '恢复会话', exact: true }).click()
  await expect(choice).toBeEnabled()
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


test('compact workspace reuses management configuration and keeps the footer reachable', async ({ page }, testInfo) => {
  const backend = await installWorkspace(page, { configuration: true })
  await page.route('**/configuration/agent', route => route.fulfill({ contentType: 'text/html', body: '<main>Existing Agent configuration</main>' }))
  await page.goto('/module-ui/agent/sessions/61')
  await expect(page.locator('.sidebar-header .el-switch')).toHaveCount(0)
  const home = await page.getByRole('button', { name: '能力首页', exact: true }).boundingBox()
  const create = await page.getByRole('button', { name: '新对话', exact: true }).boundingBox()
  expect(Math.abs(home.y + home.height / 2 - create.y - create.height / 2)).toBeLessThan(2)
  const plan = page.locator('.messages-area .agent-plan')
  await expect(plan).not.toHaveAttribute('open')
  await expect(plan).toContainText('已处理 1/2')
  await page.getByRole('button', { name: '过程与成果', exact: true }).click()
  await expect(page.getByRole('button', { name: '收起侧栏', exact: true })).toBeVisible()
  await expect(page.locator('.step-summary')).not.toHaveAttribute('open')
  await page.screenshot({ path: testInfo.outputPath('workspace-enhancement-wide.png') })
  await page.getByRole('button', { name: '收起侧栏', exact: true }).click()
  await page.setViewportSize({ width: 620, height: 900 })
  await page.evaluate(() => document.documentElement.className = 'dark')
  await expect(page.getByRole('button', { name: '设置', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('workspace-enhancement-narrow.png') })
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await page.getByRole('menuitem', { name: '管理配置', exact: true }).click()
  await expect(page).toHaveURL(/\/configuration\/agent$/)
  await expect(page.getByText('Existing Agent configuration')).toBeVisible()
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})

test('browser preferences persist and remain separate across tenants and accounts', async ({ page }) => {
  const identity = { user: 9, tenant: '7' }
  const backend = await installWorkspace(page, { identity })
  async function openPreferences() {
    await page.getByRole('button', { name: '设置', exact: true }).click()
    await expect(page.getByRole('menuitem', { name: '管理配置', exact: true })).toHaveCount(0)
    await page.getByRole('menuitem', { name: '使用偏好', exact: true }).click()
    return page.getByRole('dialog', { name: '使用偏好', exact: true })
  }
  await page.goto('/module-ui/agent/sessions/61')
  let dialog = await openPreferences()
  await dialog.getByRole('combobox', { name: '回复详略' }).press('ArrowDown')
  await page.getByRole('option', { name: '简洁', exact: true }).click()
  await dialog.getByRole('combobox', { name: '发送快捷键' }).press('ArrowDown')
  await page.getByRole('option', { name: 'Enter', exact: true }).click()
  await dialog.getByText('默认展开计划与技术详情', { exact: true }).click()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('.messages-area .agent-plan')).toHaveJSProperty('tagName', 'SECTION')
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('addp:agent:preferences:v1:9:7')))).toEqual({ replyDetail: 'concise', expandDetails: true, sendShortcut: 'enter' })
  await page.reload()
  await expect(page.locator('textarea')).toHaveAttribute('placeholder', /Enter 发送/)
  identity.tenant = '8'
  await page.reload()
  dialog = await openPreferences()
  await expect(dialog.locator('.el-form-item').filter({ has: page.getByRole('combobox', { name: '回复详略' }) })).toContainText('标准')
  await expect(dialog.getByRole('checkbox')).not.toBeChecked()
  await dialog.getByRole('button', { name: '关闭', exact: true }).click()
  identity.tenant = '7'; identity.user = 10
  await page.reload()
  await expect(page.locator('textarea')).toHaveAttribute('placeholder', /Ctrl\+Enter/)
  identity.user = 9
  await page.reload()
  await expect(page.locator('textarea')).toHaveAttribute('placeholder', /Enter 发送/)
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})

test('find, copy, export and text-only new conversation never replay hidden state', async ({ page, context }, testInfo) => {
  const history = [{ id: 1, role: 'user', content: '只读分析，不创建、不运行' }, { ...resultMessage, parts: [
    { type: 'text', text: '查询草稿\n```sql\nSELECT 1;\n```' }, ...resultMessage.parts,
    { type: 'interaction_ref', interaction_id: 'private-approval', kind: 'owner_approval', status: 'pending', payload: { secret: 'private-checkpoint' } }
  ] }]
  const backend = await installWorkspace(page, { history })
  await page.goto('/module-ui/agent/sessions/61')
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(page.url()).origin })
  await page.getByRole('button', { name: '查找当前会话', exact: true }).click()
  await page.getByPlaceholder('查找已加载的会话文字').fill('SELECT 1')
  const reply = page.locator('[data-message-id="result-71"]')
  await expect(reply).toHaveClass(/find-match/)
  await expect(reply).toBeFocused()
  await reply.getByRole('button', { name: '复制代码', exact: true }).click()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('SELECT 1;')
  await reply.getByRole('button', { name: '复制文字', exact: true }).click()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('```sql')
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出 Markdown', exact: true }).click()
  const download = await downloadPromise
  const exported = await readFile(await download.path(), 'utf8')
  expect(exported).toContain('SELECT 1;')
  expect(exported).not.toContain('private-')
  expect(exported).not.toContain('agent_run_id')
  await page.getByRole('button', { name: '过程与成果', exact: true }).click()
  const panel = page.getByRole('complementary', { name: '过程与成果' })
  await panel.getByRole('tab', { name: '会话信息', exact: true }).click()
  await expect(panel.getByText('只读分析，不创建、不运行', { exact: true })).toBeVisible()
  await panel.getByRole('button', { name: '待审批：定位原交互', exact: true }).click()
  await expect(reply).toBeFocused()
  await panel.getByRole('button', { name: '关闭', exact: true }).click()
  await page.locator('textarea').fill('已有草稿')
  await reply.getByRole('button', { name: '另开会话', exact: true }).click()
  const dialog = page.locator('.el-dialog').filter({ hasText: '仅将所选文字放入可编辑草稿' })
  await dialog.getByRole('textbox').fill('只解释这个 SQL，不执行。')
  await dialog.getByRole('button', { name: '使用此草稿', exact: true }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.locator('.el-message-box')).not.toBeVisible()
  await expect(page.locator('.input-area textarea')).toHaveValue('已有草稿')
  await dialog.getByRole('button', { name: '使用此草稿', exact: true }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '使用此草稿', exact: true }).click()
  await expect(page).toHaveURL(/\/module-ui\/agent\/$/)
  await expect(page.locator('.input-area textarea')).toHaveValue('只解释这个 SQL，不执行。')
  await expect(page.getByRole('heading', { name: '你好！我是 ADDP AI 助手。' })).toBeVisible()
  await expect(page.locator('.message-row')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('text-only-conversation-draft.png') })
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})



async function installIframeHost(page, testInfo, { delayDraft = false } = {}) {
  const root = resolve(dirname(testInfo.file), '../../../common-frontend/basic/src')
  const bridge = (await readFile(resolve(root, 'utils/consoleBridge.js'), 'utf8')).replace(/^export /gm, '')
  const drafts = (await readFile(resolve(root, 'utils/navigationDraft.mjs'), 'utf8')).replace(/^import[^\n]+\n/gm, '').replace(/^export /gm, '')
  const auth = (await readFile(resolve(root, 'auth/authSession.js'), 'utf8')).replace(/^export /gm, '')
  await page.route('**/workspace-host', route => route.fulfill({ contentType: 'text/html', body: `
    <p id="ready"></p><p id="destination"></p><p id="draft-waiting"></p><button id="release-draft">Release draft</button>
    <iframe title="Agent workspace" src="/module-ui/agent/sessions/61" style="width:100%;height:850px"></iframe>
    <script type="module">
      ${bridge}
      ${drafts}
      (function () {
        ${auth}
        createIframeAuthCoordinator({ allowedOrigins: [location.origin], getToken: () => 'fixture-token', getExpiresAt: () => Date.now() + 300000, refreshToken: async () => 'fixture-token', logout: async () => {} });
      })();
      let releaseDraft;
      const draftReady = new Promise(resolve => { releaseDraft = resolve });
      document.querySelector('#release-draft').onclick = releaseDraft;
      const slot = createNavigationDraftSlot();
      let activeRoute = '/agent/sessions/61';
      registerConsoleBridgeHandler('console-navigation', async payload => {
        if (payload.operation === 'take-draft') {
          if (${delayDraft} && activeRoute === '/agent') {
            document.querySelector('#draft-waiting').textContent = 'Waiting for draft';
            await draftReady;
          }
          return slot.take(activeRoute, 'fixture-user-tenant');
        }
        if (payload.draft) slot.publish(payload.route, 'fixture-user-tenant', payload.draft);
        activeRoute = payload.route;
        document.querySelector('#destination').textContent = payload.route;
        if (payload.draft) document.querySelector('iframe').src = '/module-ui/agent/';
        return { route: payload.route };
      }, { allowedSources: ['addp-module'], acknowledgePending: true });
      document.querySelector('#ready').textContent = 'Bridge ready';
    </script>` }))
  await page.goto('/workspace-host')
  await expect(page.locator('#ready')).toHaveText('Bridge ready')
  return page.frameLocator('iframe')
}

test('configuration shortcut uses the existing trusted source through the real iframe bridge', async ({ page }, testInfo) => {
  const backend = await installWorkspace(page, { configuration: true })
  const frame = await installIframeHost(page, testInfo)
  await frame.getByRole('button', { name: '新对话', exact: true }).waitFor()
  await frame.getByRole('button', { name: '设置', exact: true }).click()
  await frame.getByRole('menuitem', { name: '管理配置', exact: true }).click()
  await expect(page.locator('#destination')).toHaveText('/configuration/agent')
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})

test('text draft survives a real iframe remount through the shared single-use slot', async ({ page }, testInfo) => {
  const backend = await installWorkspace(page, { history: [{ id: 1, role: 'assistant', content: '所选文字，不执行' }] })
  const frame = await installIframeHost(page, testInfo)
  await frame.getByRole('button', { name: '另开会话', exact: true }).click()
  await frame.getByRole('dialog', { name: '另开会话', exact: true }).getByRole('button', { name: '使用此草稿', exact: true }).click()
  await expect(page.locator('#destination')).toHaveText('/agent')
  await expect(frame.locator('.input-area textarea')).toHaveValue('所选文字，不执行')
  await expect(frame.locator('.message-row')).toHaveCount(0)
  await page.locator('iframe').evaluate(element => element.contentWindow.location.reload())
  await expect(frame.locator('.input-area textarea')).toHaveValue('')
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})


test('delayed draft transfer keeps editing and session changes closed until text is ready', async ({ page }, testInfo) => {
  const backend = await installWorkspace(page, { history: [{ id: 1, role: 'assistant', content: '所选文字，不执行' }] })
  const frame = await installIframeHost(page, testInfo, { delayDraft: true })
  await frame.getByRole('button', { name: '另开会话', exact: true }).click()
  await frame.getByRole('dialog', { name: '另开会话', exact: true }).getByRole('button', { name: '使用此草稿', exact: true }).click()
  await expect(page.locator('#draft-waiting')).toHaveText('Waiting for draft')
  await expect(frame.locator('.input-area textarea')).toBeDisabled()
  await expect(frame.getByRole('button', { name: '发送', exact: true })).toHaveCount(0)
  await expect(frame.getByRole('button', { name: '新对话', exact: true })).toBeDisabled()
  await frame.getByRole('button', { name: '数据分析会话', exact: true }).click()
  await expect(frame.getByRole('heading', { name: '你好！我是 ADDP AI 助手。' })).toBeVisible()
  await page.locator('#release-draft').click()
  await expect(frame.locator('.input-area textarea')).toHaveValue('所选文字，不执行')
  await expect(frame.locator('.input-area textarea')).toBeEnabled()
  await frame.locator('.input-area textarea').fill('用户继续编辑的文字')
  await expect(frame.locator('.input-area textarea')).toHaveValue('用户继续编辑的文字')
  await expect(frame.locator('.message-row')).toHaveCount(0)
  expect(backend.writes).toEqual([])
  expect(backend.unexpected).toEqual([])
})
