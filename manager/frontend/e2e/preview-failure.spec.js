import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const itemLocator = name => `addp://engine/2/path/example/${name}?type=table&item_id=${name === 'first' ? 8 : 10}`
const itemNode = name => ({ id: itemLocator(name), locator: itemLocator(name), label: name, type: 'table', hasChildren: false })

for (const locale of ['zh-cn', 'en']) {
  test(`shows loading until the preview response arrives in ${locale}`, async ({ page }) => {
    let releasePreview
    const previewPending = new Promise(resolve => { releasePreview = resolve })
    await installBackend(page, locale, 200, '', previewPending)
    await page.goto(`/data-explorer?locator=${encodeURIComponent(itemLocator('first'))}`)
    const loading = page.getByTestId('preview-loading')
    try {
      await expect(loading).toHaveText(locale === 'zh-cn' ? '加载中...' : 'Loading...')
      await expect(page.getByText(locale === 'zh-cn' ? '暂无可预览数据' : 'No preview data available', { exact: true })).toHaveCount(0)
    } finally {
      releasePreview()
    }
    await expect(loading).toHaveCount(0)
    await expect(page.locator('.preview-panel .el-table__empty-text')).toBeVisible()
  })

  for (const [status, code, zh, en] of [
    [403, '', '无权读取此数据项', 'No permission to read this data item'],
    [403, 'security_protection_required', '数据保护阻止预览', 'Preview blocked by data protection'],
    [403, 'another_denial', '无权读取此数据项', 'No permission to read this data item'],
    [503, '', '预览服务暂时不可用', 'Preview service temporarily unavailable'],
    [503, 'security_protection_required', '预览服务暂时不可用', 'Preview service temporarily unavailable'],
    [504, 'preview_connection_acquisition_timeout', '获取数据库连接超时', 'Timed out acquiring a database connection'],
    [504, 'preview_sql_execution_timeout', '数据库 SQL 执行超时', 'The database SQL execution timed out'],
    [504, 'preview_timeout', '查询超过了允许的执行时间', 'The query exceeded its execution time limit'],
    [409, 'preview_source_refresh_required', '请刷新元数据后继续预览', 'Refresh metadata to continue previewing'],
    [409, 'other_conflict', '数据预览失败', 'Data preview failed'],
    [503, 'preview_source_unavailable', '源文件版本暂时无法核验', 'Source version temporarily unverifiable'],
    [422, 'preview_text_encoding_invalid', '源数据文本编码无效', 'Invalid text encoding'],
    [500, '', '数据预览失败', 'Data preview failed']
  ]) {
    test(`keeps HTTP ${status} ${code || 'without code'} visible instead of an empty preview in ${locale}`, async ({ page }) => {
      await installBackend(page, locale, status, code)
      await page.goto(`/data-explorer?locator=${encodeURIComponent(itemLocator('first'))}`)
      const failure = page.getByTestId('preview-failure')
      await expect(failure).toBeVisible()
      await expect(failure).toContainText(locale === 'zh-cn' ? zh : en)
      if (status === 504) {
        await expect(page.locator('.el-message--error')).toHaveCount(0)
        await expect(failure).not.toContainText('Preview request failed')
      }
      if (status === 403 && code === 'security_protection_required') {
        await expect(failure).toContainText(locale === 'zh-cn' ? '保护规则、同步状态和有效期' : 'protection rules, synchronization status, and validity period')
        await expect(failure).not.toContainText(locale === 'zh-cn' ? '联系授权管理员' : 'authorization administrator')
      } else if (status === 403) {
        await expect(failure).toContainText(locale === 'zh-cn' ? '核对功能权限和数据授权' : 'functional permissions and data grants')
      }
      if (code === 'preview_source_refresh_required') {
        await expect(failure).toContainText(locale === 'zh-cn' ? '刷新当前数据项的元数据' : "Refresh this data item's metadata")
        await expect(failure).toContainText(locale === 'zh-cn' ? '重建索引' : 'rebuild the index')
      }
      if (code === 'preview_text_encoding_invalid') {
        await expect(failure).toContainText(locale === 'zh-cn' ? '二进制类型声明' : 'binary type declaration')
      }
      await expect(page.getByText(locale === 'zh-cn' ? '暂无可预览数据' : 'No preview data available', { exact: true })).toHaveCount(0)
      await page.reload()
      await expect(failure).toBeVisible()

      // A later successful empty response must clear the denial, not retain a false failure.
      await page.locator('.el-tree-node__content').filter({ hasText: /^\s*second\s*$/ }).click()
      await expect(page).toHaveURL(/second/)
      await expect(failure).toHaveCount(0)
      await expect(page.locator('.preview-panel .el-table__empty-text')).toBeVisible()
    })
  }
}

async function installBackend(page, locale, status, code, previewPending) {
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const root = { id: rootLocator, locator: rootLocator, label: 'Preview test engine', type: 'server', hasChildren: true, children: [itemNode('first'), itemNode('second')] }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'preview-failure-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'preview-e2e', local_account: { username: 'preview-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: itemLocator('first'), ancestors: [{ ...root, children: [] }, itemNode('first')] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      if (url.searchParams.get('locator') === itemLocator('first')) {
        if (previewPending) await previewPending
        if (status !== 200) return json(route, { error: 'Preview request failed', error_code: code }, status)
      }
      return json(route, { preview_type: 'table', data: { mode: 'table', columns: ['id'], rows: [], total: 0 } })
    }
    return json(route, {})
  })
}

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

for (const locale of ['zh-cn', 'en']) for (const [boundary, leave] of [['create', false], ['status', false], ['download', false], ['create', true]]) {
  test(`obsolete Manager export does not affect another resource (${boundary}, ${leave ? 'leave' : 'switch'}, ${locale})`, async ({ page }) => {
    await installBackend(page, locale, 200, '')
    await page.route('**/api/v1/meta/resource-tree/2/ancestors?*', route => {
      const locator = new URL(route.request().url()).searchParams.get('locator') || itemLocator('first')
      const name = locator.includes('/second?') ? 'second' : 'first'
      return json(route, { target_locator: locator, ancestors: [{ id: 'addp://engine/2/path/?type=server&node_id=1', locator: 'addp://engine/2/path/?type=server&node_id=1', label: 'Preview test engine', type: 'server' }, itemNode(name)] })
    })
    const zh = locale === 'zh-cn', calls = [], downloads = [], errors = []
    let blocked = false, release, settled = false, blockedRequest, transportSettled = false
    const pending = new Promise(resolve => { release = resolve })
    page.on('requestfinished', request => { if (request === blockedRequest) transportSettled = true })
    page.on('requestfailed', request => { if (request === blockedRequest) transportSettled = true })
    page.on('download', item => downloads.push(item.suggestedFilename()))
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/api/v1/manager/resource-actions?*', route => json(route, { actions: { export: { supported: true, formats: ['csv'] } } }))
    const handle = async (route, stage) => {
      calls.push(stage)
      if (stage === 'create') expect(route.request().postDataJSON().source_item_locator).toBe(itemLocator(calls.filter(x => x === 'create').length === 1 ? 'first' : 'second'))
      if (boundary === stage) { blockedRequest = route.request(); blocked = true; await pending }
      try {
        if (stage === 'download') await route.fulfill({ status: 200, contentType: 'text/csv', body: 'id,phone\n1,***\n' })
        else await json(route, stage === 'create' ? { id: 91 } : { id: 91, status: 'success', file_name: 'protected.csv', download_url: '/api/v1/manager/exports/91/file' })
      } catch (error) { if (!route.request().failure()) throw error }
      finally { if (boundary === stage) settled = true }
    }
    await page.route('**/api/v1/manager/exports', route => handle(route, 'create'))
    await page.route('**/api/v1/manager/exports/91', route => handle(route, 'status'))
    await page.route('**/api/v1/manager/exports/91/file', route => handle(route, 'download'))
    await page.goto(`/data-explorer?locator=${encodeURIComponent(itemLocator('first'))}`)
    const exportLabel = zh ? '导出全部结果' : 'Export All Results'
    await page.getByRole('button', { name: exportLabel, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: exportLabel, exact: true })
    await dialog.getByRole('button', { name: zh ? '开始导出' : 'Start Export', exact: true }).click()
    await expect.poll(() => blocked).toBe(true)
    await page.locator('.data-explorer').evaluate((el, next) => { void el.__vueParentComponent.proxy.$router.push(next) }, leave ? '/data-retrieval' : `/data-explorer?locator=${encodeURIComponent(itemLocator('second'))}`)
    if (leave) await expect(page.locator('.data-explorer')).toHaveCount(0)
    else await expect(page).toHaveURL(/second/)
    await expect(dialog).not.toBeVisible()
    release()
    await expect.poll(() => settled && transportSettled).toBe(true)
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    expect(downloads).toEqual([])
    expect(calls).toEqual(boundary === 'create' ? ['create'] : boundary === 'status' ? ['create', 'status'] : ['create', 'status', 'download'])
    expect(errors).toEqual([])
    if (!leave) {
      await page.getByRole('button', { name: exportLabel, exact: true }).click()
      const start = dialog.getByRole('button', { name: zh ? '开始导出' : 'Start Export', exact: true })
      await expect(start).toBeEnabled()
      const downloaded = page.waitForEvent('download')
      await start.click()
      expect((await downloaded).suggestedFilename()).toBe('protected.csv')
      expect(downloads).toHaveLength(1)
      expect(errors).toEqual([])
    }
  })
}
