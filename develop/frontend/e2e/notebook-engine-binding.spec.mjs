import { expect, test } from '@playwright/test'

const NOTEBOOK_ID = 14
const NOTEBOOK_ENGINE = {
  id: 10,
  name: 'Jupyter Engine',
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

async function installMockBackend(page) {
  const notebook = {
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
      engine_id: NOTEBOOK_ENGINE.id
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
      return fulfillJSON(route, [NOTEBOOK_ENGINE])
    }
    if (path === `/api/v1/develop/notebook-engines/${NOTEBOOK_ENGINE.id}/kernels`) {
      return fulfillJSON(route, {
        kernels: [{ name: 'python3', display_name: 'Python 3', language: 'python' }]
      })
    }
    if (path === '/api/v1/develop/notebooks') {
      return fulfillJSON(route, { items: [notebook], total: 1, page: 1, page_size: 20 })
    }
    if (path === `/api/v1/develop/task-definitions/${NOTEBOOK_ID}`) {
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
