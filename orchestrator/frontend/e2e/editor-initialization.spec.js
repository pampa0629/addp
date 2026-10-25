import { expect, test } from '@playwright/test'

for (const embedded of [false, true]) {
  test(`new orchestration initializes once (${embedded ? 'Console iframe' : 'standalone'})`, async ({ page }) => {
    const backend = await installBackend(page)
    const errors = []
    page.on('pageerror', error => errors.push(error.stack))
    const editor = await openEditor(page, embedded)
    await expect(editor.locator('#dag-container canvas')).toBeVisible()
    await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
    await settleEditor(editor)
    // One request from TaskPanel and one from DAGEditor; no discarded first mount.
    expect(backend.providerRequests()).toBe(2)
    expect(errors).toEqual([])
  })
}

test('leaving a new orchestration while metadata is pending does not resume its destroyed canvas', async ({ page }) => {
  const backend = await installBackend(page, { deferProviders: true })
  const errors = []
  page.on('pageerror', error => errors.push(error.stack))
  await page.goto('/orchestrations/new')
  await expect(page.locator('#dag-container canvas')).toBeVisible()
  await expect.poll(backend.providerRequests).toBe(2)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page).toHaveURL(/\/orchestrations$/)
  await expect(page.locator('#dag-container')).toHaveCount(0)
  await backend.releaseProviders()
  await settleEditor(page)
  expect(errors).toEqual([])

  // Returning creates a usable editor with its own initialization state.
  await page.getByRole('button', { name: '创建编排', exact: true }).click()
  await expect(page.locator('#dag-container canvas')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await settleEditor(page)
  expect(errors).toEqual([])
})

for (const rejected of [false, true]) {
  test(`leaving an editor ignores its pending execution contract (${rejected ? 'rejected' : 'resolved'})`, async ({ page }) => {
    const backend = await installBackend(page, { deferContract: true, rejectContract: rejected })
    const errors = []
    page.on('pageerror', error => errors.push(error.stack))
    page.on('console', message => {
      if (message.type() === 'error' && message.text().includes('加载任务执行契约失败')) errors.push(message.text())
    })
    await page.goto('/orchestrations/initialization/edit')
    await expect.poll(backend.contractRequests).toBe(1)
    await expect(page.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
    await page.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
    await expect(page).toHaveURL(/\/orchestrations$/)
    await backend.releaseContract()
    await settleEditor(page)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    expect(errors).toEqual([])
    await page.getByRole('button', { name: '创建编排', exact: true }).click()
    await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
    await expect(page.locator('.unsaved-status')).toHaveCount(0)
    expect(errors).toEqual([])
  })
}

for (const rejected of [false, true]) {
  for (const newReady of [false, true]) {
    test(`old ${rejected ? 'failed' : 'successful'} contract settles after new iframe canvas mounts (${newReady ? 'ready' : 'pending'})`, async ({ page }) => {
      const backend = await installBackend(page, { deferContract: true, rejectContract: rejected, overlap: true })
      const errors = []
      const writes = []
      page.on('pageerror', error => errors.push(error.message))
      page.on('console', message => {
        if (message.type() === 'error' && message.text().includes('加载任务执行契约失败')) errors.push(message.text())
      })
      page.on('request', request => {
        if (request.url().includes('/api/v1/orchestrator/') && request.method() !== 'GET') writes.push(request.method())
      })
      const editor = await openEditor(page, true, '/orchestrations/initialization/edit')
      await expect.poll(backend.contractRequests).toBe(1)
      await expect(editor.locator('#dag-container canvas')).toBeVisible()
      await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
      const oldCanvas = await editor.locator('#dag-container canvas').elementHandle()
      const documentHandle = await oldCanvas.evaluateHandle(element => element.ownerDocument)

      await editor.locator('.header-actions').getByRole('button', { name: '取消', exact: true }).click()
      await expect(editor.locator('#dag-container')).toHaveCount(0)
      await editor.getByRole('row').filter({ hasText: 'Returning editor' }).getByRole('button', { name: '编辑', exact: true }).click()
      await expect.poll(backend.contractRequests).toBe(2)
      await expect(editor.locator('#dag-container canvas')).toBeVisible()
      const newCanvas = await editor.locator('#dag-container canvas').elementHandle()
      await expect(page).toHaveURL(/\/orchestrator\/orchestrations\/returning\/edit$/)
      expect(await oldCanvas.evaluate(element => element.isConnected)).toBe(false)
      expect(await newCanvas.evaluate((element, document) => element.ownerDocument === document, documentHandle)).toBe(true)
      await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()

      // Independently gate the new editor, so an old completion cannot make it ready.
      if (newReady) await backend.releaseNextContract()
      if (newReady) await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
      const before = await readDefinition(editor)
      const responsePromise = page.waitForResponse(response => response.url().endsWith('/task-providers/meta/tasks/scan/1'))
      await backend.releaseContract()
      const response = await responsePromise
      expect(response.status()).toBe(rejected ? 502 : 200)
      await response.finished()
      await settleEditor(editor)
      expect(await newCanvas.evaluate(element => element.isConnected && element === document.querySelector('#dag-container canvas'))).toBe(true)
      expect(await readDefinition(editor)).toEqual(before)
      expect(await editor.getByRole('button', { name: '保存', exact: true }).isEnabled()).toBe(newReady)
      await expect(editor.locator('.el-message--error')).toHaveCount(0)
      expect(errors).toEqual([])

      if (!newReady) await backend.releaseNextContract()
      await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
      const definition = await readDefinition(editor)
      expect(definition.steps).toEqual(backend.returningDefinition.steps)
      expect(definition.editor_layout).toEqual(backend.returningDefinition.editor_layout)
      await expect(editor.locator('.unsaved-status')).toHaveCount(0)
      expect(writes).toEqual([])
      expect(errors).toEqual([])
      await oldCanvas.dispose()
      await newCanvas.dispose()
      await documentHandle.dispose()
    })
  }
}

async function readDefinition(editor) {
  await editor.locator('.header-actions').getByRole('button', { name: '查看 JSON', exact: true }).click()
  const definition = JSON.parse(await editor.locator('.json-content').innerText())
  await editor.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await expect(editor.getByRole('dialog')).toBeHidden()
  return definition
}

async function installBackend(page, { deferProviders = false, deferContract = false, rejectContract = false, overlap = false } = {}) {
  let requests = 0
  let contractRequests = 0
  let release
  const gate = new Promise(resolve => { release = resolve })
  let releaseContract
  const contractGate = new Promise(resolve => { releaseContract = resolve })
  let releaseNextContract
  const nextContractGate = new Promise(resolve => { releaseNextContract = resolve })
  let contractResponse
  let nextContractResponse
  const returningDefinition = {
    id: 'returning', name: 'Returning editor', enabled: false, schedule: '',
    steps: [{ id: 'returning-step', name: 'Returning step', provider: 'meta', task_type: 'scan', task_id: 2, parameters: { scope: 'new' }, depends_on: [], timeout: 300 }],
    editor_layout: { nodes: { 'returning-step': { x: 240, y: 180 } }, viewport: { zoom: 1, translate_x: 0, translate_y: 0 } }
  }
  const pending = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: [] } }))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return route.fulfill({ json: { access_token: 'initialization-fixture', expires_in: 3600 } })
    if (path.endsWith('/system/users/me')) return route.fulfill({ json: { id: 1, username: 'initialization-reader' } })
    if (path.endsWith('/system/auth/context')) return route.fulfill({ json: { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: overlap ? ['orchestrator.workflow.read', 'orchestrator.workflow.update'] : ['orchestrator.workflow.read', 'orchestrator.workflow.create', 'orchestrator.workflow.update'] }] } } })
    if (path.endsWith('/orchestrator/task-providers')) {
      requests += 1
      const response = (async () => {
        if (deferProviders) await gate
        await route.fulfill({ json: [] })
      })()
      pending.push(response)
      return response
    }
    if (path.endsWith('/orchestrator/orchestrations/initialization')) return route.fulfill({ json: {
      id: 'initialization', name: 'Pending contract', editor_layout: {},
      steps: [{ id: 'pending-step', name: 'Pending step', provider: 'meta', task_type: 'scan', task_id: 1, parameters: {}, depends_on: [] }]
    } })
    if (path.endsWith('/orchestrator/orchestrations/returning')) return route.fulfill({ json: returningDefinition })
    if (/\/orchestrator\/task-providers\/meta\/tasks\/scan\/[12]$/.test(path)) {
      contractRequests += 1
      const next = path.endsWith('/2')
      const response = (async () => {
        if (next) await nextContractGate
        else if (deferContract) await contractGate
        await route.fulfill({ status: !next && rejectContract ? 502 : 200, json: !next && rejectContract ? { error: 'Contract unavailable', error_code: 'execution_contract_unavailable' } : { execution_contract: { input_schema: { type: 'object', properties: { scope: { type: 'string' } }, additionalProperties: false }, input_defaults: {}, input_ui_schema: {}, output_schema: { type: 'object', properties: {}, additionalProperties: false } } } })
      })()
      if (next) nextContractResponse = response
      else contractResponse = response
      return response
    }
    if (path.endsWith('/orchestrator/tasks')) return route.fulfill({ json: { items: [], total: 0 } })
    if (path.endsWith('/orchestrator/orchestrations')) return route.fulfill({ json: overlap ? [returningDefinition] : [] })
    throw new Error(`Unexpected initialization fixture request: ${path}`)
  })
  return {
    providerRequests: () => requests,
    contractRequests: () => contractRequests,
    returningDefinition,
    releaseProviders: async () => {
      deferProviders = false
      release()
      await Promise.all(pending)
    },
    releaseContract: async () => {
      releaseContract()
      await contractResponse
    },
    releaseNextContract: async () => {
      releaseNextContract()
      await nextContractResponse
    }
  }
}

async function openEditor(page, embedded, path = '/orchestrations/new') {
  if (!embedded) {
    await page.goto(path)
    return page
  }
  const shared = new URL('../../../common-frontend/basic/src/', import.meta.url).pathname
  await page.route('**/editor-initialization-harness', route => route.fulfill({ contentType: 'text/html', body: `
    <style>html,body{margin:0;height:100%;overflow:hidden}iframe{width:100%;height:100%;border:0}</style>
    <script type="module">
      import { createIframeAuthCoordinator } from '/@fs${shared}auth/authSession.js'
      import { registerConsoleBridgeHandler } from '/@fs${shared}utils/consoleBridge.js'
      import { CONSOLE_NAVIGATION_CHANNEL } from '/@fs${shared}utils/taskOwnerUrl.js'
      createIframeAuthCoordinator({ allowedOrigins:[location.origin], getToken:()=> 'initialization-fixture',
        getExpiresAt:()=> Date.now()+300000, refreshToken:async()=> 'initialization-fixture', logout:async()=>{} })
      const iframe = document.createElement('iframe')
      registerConsoleBridgeHandler(CONSOLE_NAVIGATION_CHANNEL, (payload, message, event) => {
        if (event.source !== iframe.contentWindow || event.origin !== location.origin || !payload.synchronized) {
          throw new Error('Expected same-origin synchronized module navigation')
        }
        history[payload.history === 'push' ? 'pushState' : 'replaceState']({}, '', payload.route)
        return { cancelled: false }
      })
      iframe.src = ${JSON.stringify(path)}
      document.body.append(iframe)
    </script>` }))
  await page.goto('/editor-initialization-harness')
  return page.frameLocator('iframe')
}

async function settleEditor(editor) {
  await editor.locator('body').evaluate(() => new Promise(resolve => {
    requestAnimationFrame(() => requestAnimationFrame(resolve))
  }))
}
