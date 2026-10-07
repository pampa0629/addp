import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const itemLocator = name => `addp://engine/2/path/example/${name}?type=table&item_id=${name === 'first' ? 8 : 10}`
const itemNode = name => ({ id: itemLocator(name), locator: itemLocator(name), label: name, type: 'table', hasChildren: false })

for (const locale of ['zh-cn', 'en']) {
  for (const [status, zh, en] of [
    [403, '无权读取此数据项', 'No permission to read this data item'],
    [503, '预览服务暂时不可用', 'Preview service temporarily unavailable'],
    [500, '数据预览失败', 'Data preview failed']
  ]) {
    test(`keeps HTTP ${status} visible instead of an empty preview in ${locale}`, async ({ page }) => {
      await installBackend(page, locale, status)
      await page.goto(`/data-explorer?locator=${encodeURIComponent(itemLocator('first'))}`)
      const failure = page.getByTestId('preview-failure')
      await expect(failure).toBeVisible()
      await expect(failure).toContainText(locale === 'zh-cn' ? zh : en)
      if (status === 403) {
        await expect(failure).toContainText(locale === 'zh-cn' ? '核对功能权限和数据授权' : 'functional permissions and data grants')
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

async function installBackend(page, locale, status) {
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const root = { id: rootLocator, locator: rootLocator, label: 'Preview test engine', type: 'server', hasChildren: true, children: [itemNode('first'), itemNode('second')] }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'preview-failure-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'preview-e2e', local_account: { username: 'preview-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: itemLocator('first'), ancestors: [{ ...root, children: [] }, itemNode('first')] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      if (url.searchParams.get('locator') === itemLocator('first')) return json(route, { error: 'Preview request failed' }, status)
      return json(route, { preview_type: 'table', data: { mode: 'table', columns: ['id'], rows: [], total: 0 } })
    }
    return json(route, {})
  })
}

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
