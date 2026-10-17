import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
const schemaLocator = 'addp://engine/2/path/public?type=schema&node_id=2'
const transferID = 'e15e1b10-196d-416e-8b50-97e4a9657475'
const scanID = '9f145d6f-cd67-401a-a31d-0744f356a8c3'
const privateMessage = 'PRIVATE_RUNTIME_DETAIL addp-infra://minio/private/source password=sentinel'
const failureDetails = { category: 'execution_failed', code: 'data_protection_required', diagnostic: {
  schema_version: 'addp.execution-failure/v2', stage: 'preparation', reason: 'data_protection_required'
} }

for (const locale of ['zh-cn', 'en']) {
  for (const status of ['failed', 'timeout', 'cancelled']) {
    test(`import shows safe ${status} diagnostics and stops polling (${locale})`, async ({ page }) => {
      const state = await installBackend(page, locale, { status, error_details: failureDetails, error_msg: privateMessage })
      const dialog = await submitImport(page, locale)
      const failure = dialog.locator('.execution-failure')
      await expect(failure).toBeVisible()
      await expect(failure).toContainText('data_protection_required')
      await expect(failure).toContainText(transferID)
      await expect(failure).toContainText(locale === 'zh-cn' ? '保护规则' : 'protection rules')
      await expect(failure).not.toContainText(locale === 'zh-cn' ? '本次查询' : 'The query')
      await expect(dialog).not.toContainText(privateMessage)
      await expect(dialog.getByRole('button', { name: locale === 'zh-cn' ? '开始导入' : 'Start Import', exact: true })).toBeEnabled()
      await page.waitForTimeout(2300)
      expect(state.transferReads).toBe(1)
      expect(state.scanReads).toBe(0)
      await expect(page.locator('.el-message--success')).toHaveCount(0)
      await dialog.getByRole('button', { name: locale === 'zh-cn' ? '取消' : 'Cancel', exact: true }).click()
      await expect(dialog).toBeHidden()
    })
  }

  test(`import waits for Meta and presents its safe failure (${locale})`, async ({ page }) => {
    const state = await installBackend(page, locale, { status: 'success', metadata: {
      metadata_scan: { status: 'submitted', execution_id: scanID }
    } })
    const dialog = await submitImport(page, locale)
    await expect.poll(() => state.scanReads).toBeGreaterThan(0)
    await expect(dialog).toContainText(locale === 'zh-cn' ? '正在同步元数据' : 'Synchronizing metadata')
    await expect(page.locator('.el-message--success')).toHaveCount(0)
    state.scan = { status: 'failed', error_details: failureDetails, error_message: privateMessage }
    const failure = dialog.locator('.execution-failure')
    await expect(failure).toBeVisible()
    await expect(failure).toContainText(scanID)
    await expect(dialog).toContainText(locale === 'zh-cn' ? '数据已导入，但元数据同步未完成' : 'Data was imported, but metadata synchronization is incomplete')
    await expect(failure).not.toContainText(privateMessage)
    await expect(dialog.getByRole('button', { name: locale === 'zh-cn' ? '开始导入' : 'Start Import', exact: true })).toBeEnabled()
    const count = state.scanReads
    await page.waitForTimeout(2300)
    expect(state.scanReads).toBe(count)
    await expect(dialog).toBeVisible()
    await expect(page.locator('.el-message--success')).toHaveCount(0)
  })
}

test('import submits the current locator and clears old failure before retry', async ({ page }) => {
  const state = await installBackend(page, 'zh-cn', { status: 'failed', error_details: failureDetails })
  const dialog = await submitImport(page, 'zh-cn')
  await expect(dialog.locator('.execution-failure')).toBeVisible()
  expect(state.imports).toHaveLength(1)
  expect(state.imports[0]).toContain('name="target_node_locator"\r\n\r\n' + schemaLocator)
  expect(state.imports[0]).toContain('name="target_table"\r\n\r\nimport_points')
  expect(state.imports[0]).toContain('filename="points.shp"')
  state.transfer = { status: 'running' }
  await dialog.getByRole('button', { name: '开始导入', exact: true }).click()
  await expect(dialog.locator('.execution-failure')).toHaveCount(0)
  await expect.poll(() => state.imports.length).toBe(2)
  await expect(dialog).toContainText('正在导入数据')
  await dialog.locator('.el-dialog__headerbtn').click()
  await expect(dialog).toBeHidden()
  await page.getByRole('menuitem', { name: '数据检索', exact: true }).click()
  const count = state.transferReads
  await page.waitForTimeout(2300)
  expect(state.transferReads).toBe(count)
})

test('successful import refreshes the selected schema only after the Meta child succeeds', async ({ page }) => {
  const state = await installBackend(page, 'zh-cn', { status: 'success', metadata: {
    metadata_scan: { status: 'submitted', execution_id: scanID }
  } })
  const dialog = await submitImport(page, 'zh-cn')
  await expect.poll(() => state.scanReads).toBeGreaterThan(0)
  const before = state.schemaReads
  state.scan = { status: 'success' }
  await expect(dialog).toBeHidden()
  await expect.poll(() => state.schemaReads).toBeGreaterThan(before)
  expect(state.imports).toHaveLength(1)
})

test('a failed Meta submission cannot be reported as a successful import', async ({ page }) => {
  const state = await installBackend(page, 'zh-cn', { status: 'success', metadata: {
    metadata_scan: { status: 'failed', error_code: 'metadata_scan_failed' }
  } })
  const dialog = await submitImport(page, 'zh-cn')
  const failure = dialog.locator('.execution-failure')
  await expect(failure).toContainText('metadata_scan_failed')
  await expect(failure).toContainText(transferID)
  await expect(dialog).toContainText('数据已导入，但元数据同步未完成')
  await expect(dialog.getByRole('button', { name: '开始导入', exact: true })).toBeEnabled()
  expect(state.scanReads).toBe(0)
  await expect(page.locator('.el-message--success')).toHaveCount(0)
})

async function submitImport(page, locale) {
  await page.goto(`/data-explorer?locator=${encodeURIComponent(schemaLocator)}`)
  await page.getByRole('button', { name: locale === 'zh-cn' ? '导入数据' : 'Import Data', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('.el-form-item').filter({ hasText: locale === 'zh-cn' ? '目标引擎' : 'Target Engine' }).locator('input')).toHaveValue('Import fixture')
  await expect(dialog.locator('.el-form-item').filter({ hasText: locale === 'zh-cn' ? '目标命名空间' : 'Target Namespace' }).locator('input')).toHaveValue('public')
  const input = dialog.locator('.el-form-item').filter({ hasText: locale === 'zh-cn' ? '目标数据项名称' : 'Target Item Name' }).locator('input')
  await input.fill('import_points')
  await dialog.locator('input[type="file"]').setInputFiles({ name: 'points.shp', mimeType: 'application/octet-stream', buffer: Buffer.from('shape-fixture') })
  await dialog.getByRole('button', { name: locale === 'zh-cn' ? '开始导入' : 'Start Import', exact: true }).click()
  return dialog
}

async function installBackend(page, locale, transfer) {
  const schema = { id: schemaLocator, locator: schemaLocator, label: 'public', type: 'schema', hasChildren: true, children: [] }
  const root = { id: rootLocator, locator: rootLocator, label: 'Import fixture', type: 'server', hasChildren: true, children: [schema] }
  const state = { transfer, scan: { status: 'running' }, transferReads: 0, scanReads: 0, schemaReads: 0, imports: [] }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'import-fixture-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'import-fixture' })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: schemaLocator, ancestors: [{ ...root, children: [] }, schema] })
    if (path.endsWith('/meta/resource-tree/2')) return json(route, root)
    if (path.endsWith('/meta/resource-tree/2/node')) {
      if (url.searchParams.get('locator')?.includes('/public')) { state.schemaReads++; return json(route, schema) }
      return json(route, root)
    }
    if (path.endsWith('/manager/resource-actions')) return json(route, { actions: { import: { supported: true } } })
    if (path.endsWith('/manager/imports')) {
      state.imports.push(request.postDataBuffer().toString())
      return json(route, { transfer_task_id: 42, transfer_execution_id: transferID }, 202)
    }
    if (path.endsWith(`/transfer/executions/${transferID}`)) { state.transferReads++; return json(route, { execution_id: transferID, ...state.transfer }) }
    if (path.endsWith(`/meta/executions/${scanID}`)) { state.scanReads++; return json(route, { execution_id: scanID, ...state.scan }) }
    return json(route, {})
  })
  return state
}

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
