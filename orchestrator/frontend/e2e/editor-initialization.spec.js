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

async function installBackend(page, { deferProviders = false, deferContract = false, rejectContract = false } = {}) {
  let requests = 0
  let contractRequests = 0
  let release
  const gate = new Promise(resolve => { release = resolve })
  let releaseContract
  const contractGate = new Promise(resolve => { releaseContract = resolve })
  const pending = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: [] } }))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return route.fulfill({ json: { access_token: 'initialization-fixture', expires_in: 3600 } })
    if (path.endsWith('/system/users/me')) return route.fulfill({ json: { id: 1, username: 'initialization-reader' } })
    if (path.endsWith('/system/auth/context')) return route.fulfill({ json: { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['orchestrator.workflow.read', 'orchestrator.workflow.create', 'orchestrator.workflow.update'] }] } } })
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
    if (path.endsWith('/orchestrator/task-providers/meta/tasks/scan/1')) {
      contractRequests += 1
      const response = (async () => {
        if (deferContract) await contractGate
        await route.fulfill({ json: rejectContract ? {} : { execution_contract: { input_schema: { type: 'object', properties: {}, additionalProperties: false }, input_defaults: {}, input_ui_schema: {}, output_schema: { type: 'object', properties: {}, additionalProperties: false } } } })
      })()
      pending.push(response)
      return response
    }
    if (path.endsWith('/orchestrator/tasks')) return route.fulfill({ json: { items: [], total: 0 } })
    if (path.endsWith('/orchestrator/orchestrations')) return route.fulfill({ json: [] })
    throw new Error(`Unexpected initialization fixture request: ${path}`)
  })
  return {
    providerRequests: () => requests,
    contractRequests: () => contractRequests,
    releaseProviders: async () => {
      deferProviders = false
      release()
      await Promise.all(pending)
    },
    releaseContract: async () => {
      releaseContract()
      await Promise.all(pending)
    }
  }
}

async function openEditor(page, embedded) {
  if (!embedded) {
    await page.goto('/orchestrations/new')
    return page
  }
  const shared = new URL('../../../common-frontend/basic/src/', import.meta.url).pathname
  await page.route('**/editor-initialization-harness', route => route.fulfill({ contentType: 'text/html', body: `
    <style>html,body{margin:0;height:100%;overflow:hidden}iframe{width:100%;height:100%;border:0}</style>
    <script type="module">
      import { createIframeAuthCoordinator } from '/@fs${shared}auth/authSession.js'
      createIframeAuthCoordinator({ allowedOrigins:[location.origin], getToken:()=> 'initialization-fixture',
        getExpiresAt:()=> Date.now()+300000, refreshToken:async()=> 'initialization-fixture', logout:async()=>{} })
      const iframe = document.createElement('iframe')
      iframe.src = '/orchestrations/new'
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
