import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const { language, initiallyFailed } of [
  { language: 'zh-cn', initiallyFailed: false },
  { language: 'en', initiallyFailed: false },
  { language: 'zh-cn', initiallyFailed: true }
]) {
  test(`CSV encoding applies after refresh and failure preserves the last schema (${language}, initial failure=${initiallyFailed})`, async ({ page }) => {
    const locator = 'addp://engine/2/path/sample.csv?type=file&item_id=8'
    const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
    const item = { id: locator, locator, label: 'sample.csv', type: 'file', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Encoding engine', type: 'server', hasChildren: true, children: [item] }
    const browserErrors = []
    page.on('pageerror', error => browserErrors.push(error.message))
    const requests = []
    let encoding = 'utf-8'
    let fail = false
    let releaseRefresh
    const apply = language === 'en' ? 'Apply encoding and refresh' : '应用编码并刷新'
    await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
    await page.route('**/api/v1/**', async route => {
      const url = new URL(route.request().url())
      const path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'encoding-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'encoding', local_account: { username: 'encoding' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
      if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
      if (path.endsWith('/manager/engines/2/items/refresh')) {
        const body = route.request().postDataJSON()
        requests.push(body)
        if (fail) return json(route, { error: 'parse failed' }, 500)
        await new Promise(resolve => { releaseRefresh = resolve })
        encoding = body.encoding
        return json(route, { status: 'success' })
      }
      if (path.endsWith('/meta/items/8')) return json(route, {
        item_type: 'file', full_name: 'sample.csv', attributes: {
          item: { layout: 'single', data_type: 'table', format: 'csv' },
          format_info: { csv: { encoding } }
        }
      })
      if (path.endsWith('/manager/preview')) {
        if (initiallyFailed && encoding === 'utf-8') return json(route, { error: 'invalid UTF-8' }, 500)
        const translated = encoding === 'gbk'
        const column = translated ? '城市' : 'old_column'
        return json(route, { preview_type: 'table', data: {
          mode: 'table', columns: [column], rows: [{ [column]: translated ? '北京' : 'old_value' }], total: 1, page: 1, page_size: 20,
          object: { path: 'sample.csv', content: { kind: 'csv' } },
          item_meta: { item_type: 'file', full_name: 'sample.csv', attributes: [
            { key: 'item', value: { layout: 'single', data_type: 'table', format: 'csv' } },
            { key: 'format_info', value: { csv: { encoding } } }
          ] }
        } })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    const selector = page.locator('.csv-encoding-select')
    const button = page.getByRole('button', { name: apply, exact: true })
    if (!initiallyFailed) await expect(page.getByRole('cell', { name: 'old_value', exact: true })).toBeVisible()
    await expect(selector).toContainText('UTF-8')
    await selector.click()
    await page.getByRole('option', { name: 'GBK', exact: true }).click()
    expect(requests).toEqual([])
    await button.click()
    await expect.poll(() => requests).toEqual([{ encoding: 'gbk' }])
    if (!initiallyFailed) await expect(page.getByRole('cell', { name: 'old_value', exact: true })).toBeVisible()
    await expect(selector.locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
    releaseRefresh()
    await expect(page.getByRole('cell', { name: '北京', exact: true })).toBeVisible()
    await expect(selector).toContainText('GBK')
    await expect(page.getByRole('columnheader', { name: 'old_column', exact: true })).toHaveCount(0)
    fail = true
    await selector.click()
    await page.getByRole('option', { name: 'UTF-8', exact: true }).click()
    await button.click()
    await expect.poll(() => requests.length).toBe(2)
    await expect(selector).toContainText('GBK')
    await expect(page.getByRole('cell', { name: '北京', exact: true })).toBeVisible()
    await page.setViewportSize({ width: 1000, height: 800 })
    await expect(button).toBeVisible()
    const box = await button.boundingBox()
    expect(box.x + box.width).toBeLessThanOrEqual(1000)
    expect(browserErrors).toEqual([])
  })
}

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
