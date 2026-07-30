import { expect, test } from '@playwright/test'

const NOTEBOOK_ID = 14
const NOTEBOOK_ENGINE = {
  id: 10,
  name: 'Jupyter Engine',
  engine_type: 'jupyter',
  lifecycle_state: 'active'
}
const ALTERNATE_NOTEBOOK_ENGINE = {
  id: 11,
  name: 'Alternate Jupyter',
  engine_type: 'jupyter',
  lifecycle_state: 'active'
}

test('uses an engine-selected kernel for upload and the saved engine for execution', async ({ page }) => {
  const consoleErrors = []
  page.on('console', message => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  await installMockBackend(page)

  await page.goto(`/notebook?id=${NOTEBOOK_ID}`)
  await expect(page.getByText('scripts2', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('row', { name: 'Notebook 引擎 Jupyter Engine', exact: true })).toBeVisible()

  await page.locator('.detail-toolbar .el-button--primary').click()
  const executeDialog = page.getByRole('dialog', { name: '执行 Notebook', exact: true })
  await expect(executeDialog).toBeVisible()
  await expect(executeDialog.getByRole('textbox', { name: 'Notebook 引擎', exact: true })).toHaveValue('Jupyter Engine')
  await expect(executeDialog.getByRole('textbox', { name: 'Notebook 引擎', exact: true })).toBeDisabled()
  await expect(executeDialog.getByRole('combobox')).toHaveCount(0)
  await expect(executeDialog.getByPlaceholder(/请输入 JSON 格式的参数/)).toBeVisible()
  await executeDialog.getByRole('button', { name: '取消', exact: true }).click()

  await page.locator('.sidebar-header .el-button--primary').click()
  const uploadDialog = page.getByRole('dialog', { name: '上传 Notebook', exact: true })
  await expect(uploadDialog).toBeVisible()
  await expect(uploadDialog.getByRole('combobox')).toHaveCount(2)
  await expect(uploadDialog).toContainText('Jupyter Engine')
  await expect(uploadDialog).toContainText('Python 3')
  await expect.poll(() => consoleErrors).toEqual([])
})

test('rebinds the original Notebook task for future executions', async ({ page }) => {
  const bindingRequests = []
  await installMockBackend(page, { bindingRequests })
  await page.goto(`/notebook?id=${NOTEBOOK_ID}`)

  await page.getByRole('button', { name: '更换引擎', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '更换 Notebook 引擎', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: '当前绑定', exact: true })).toHaveValue('Jupyter Engine / python3')
  await expect(dialog).toContainText('新绑定仅用于后续执行，历史执行记录保持不变。')

  await dialog.locator('.el-select').nth(0).click()
  await page.getByRole('option', { name: 'Alternate Jupyter', exact: true }).click()
  await dialog.locator('.el-select').nth(1).click()
  await page.getByRole('option', { name: 'Python 3.12', exact: true }).click()
  await dialog.getByRole('button', { name: '确认更换', exact: true }).click()

  await expect(dialog).toBeHidden()
  await expect(page.getByRole('row', { name: 'Notebook 引擎 Alternate Jupyter', exact: true })).toBeVisible()
  await expect(page.getByRole('row', { name: 'Kernel python312', exact: true })).toBeVisible()
  expect(bindingRequests).toEqual([{ engine_id: 11, kernel: 'python312' }])
  expect(new URL(page.url()).searchParams.get('id')).toBe(String(NOTEBOOK_ID))
})

test('keeps execution disabled but allows rebinding when the saved engine is unavailable', async ({ page }) => {
  await installMockBackend(page, { boundEngineID: 8 })
  await page.goto(`/notebook?id=${NOTEBOOK_ID}`)

  await expect(page.getByRole('row', { name: 'Notebook 引擎 已失效的引擎 #8', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '执行', exact: true })).toBeDisabled()
  const changeEngine = page.getByRole('button', { name: '更换引擎', exact: true })
  await expect(changeEngine).toBeEnabled()
  await changeEngine.click()
  await expect(page.getByRole('dialog', { name: '更换 Notebook 引擎', exact: true })).toBeVisible()
})

test.describe('窄屏布局', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('stacks the list and detail without horizontal page overflow', async ({ page }) => {
    await installMockBackend(page)
    await page.goto(`/notebook?id=${NOTEBOOK_ID}`)
    await expect(page.getByText('scripts2', { exact: true }).first()).toBeVisible()

    const sidebarBox = await page.locator('.notebook-sidebar').boundingBox()
    const detailBox = await page.locator('.notebook-detail-container').boundingBox()
    expect(sidebarBox).not.toBeNull()
    expect(detailBox).not.toBeNull()
    expect(detailBox.y).toBeGreaterThanOrEqual(sidebarBox.y + sidebarBox.height - 1)

    const widths = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      bodyScrollWidth: document.body.scrollWidth
    }))
    expect(widths.scrollWidth).toBeLessThanOrEqual(widths.clientWidth)
    expect(widths.bodyScrollWidth).toBeLessThanOrEqual(widths.clientWidth)
  })
})

async function installMockBackend(page, { boundEngineID = NOTEBOOK_ENGINE.id, bindingRequests = [] } = {}) {
  let notebook = {
    id: NOTEBOOK_ID,
    tenant_id: 1,
    name: 'scripts2',
    display_name: 'scripts2',
    description: '',
    dev_type: 'script',
    updated_at: '2026-07-30T00:26:00+08:00',
    content: {
      notebook_path: 'scripts2.ipynb',
      kernel: 'python3',
      parameters: {}
    },
    execution_config: {
      engine_id: boundEngineID
    }
  }

  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/develop/notebook-engines') {
      return fulfillJSON(route, [NOTEBOOK_ENGINE, ALTERNATE_NOTEBOOK_ENGINE])
    }
    if (path === `/api/v1/develop/notebook-engines/${NOTEBOOK_ENGINE.id}/kernels`) {
      return fulfillJSON(route, {
        kernels: [{ name: 'python3', display_name: 'Python 3', language: 'python' }]
      })
    }
    if (path === `/api/v1/develop/notebook-engines/${ALTERNATE_NOTEBOOK_ENGINE.id}/kernels`) {
      return fulfillJSON(route, {
        kernels: [{ name: 'python312', display_name: 'Python 3.12', language: 'python' }]
      })
    }
    if (path === '/api/v1/develop/notebooks') {
      return fulfillJSON(route, { items: [notebook], total: 1, page: 1, page_size: 20 })
    }
    if (path === `/api/v1/develop/task-definitions/${NOTEBOOK_ID}`) {
      return fulfillJSON(route, notebook)
    }
    if (request.method() === 'PUT' && path === `/api/v1/develop/notebooks/${NOTEBOOK_ID}/runtime-binding`) {
      const binding = request.postDataJSON()
      bindingRequests.push(binding)
      notebook = {
        ...notebook,
        updated_at: '2026-07-30T10:30:00+08:00',
        content: { ...notebook.content, kernel: binding.kernel },
        execution_config: { ...notebook.execution_config, engine_id: binding.engine_id }
      }
      return fulfillJSON(route, notebook)
    }

    return fulfillJSON(route, {})
  })
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
