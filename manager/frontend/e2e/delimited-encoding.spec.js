import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const { language, initiallyFailed, format = 'csv', targetEncoding = 'gbk' } of [
  { language: 'zh-cn', initiallyFailed: false },
  { language: 'en', initiallyFailed: false },
  { language: 'zh-cn', initiallyFailed: true },
  { language: 'en', initiallyFailed: false, format: 'tsv' },
  { language: 'zh-cn', initiallyFailed: true, targetEncoding: 'gb18030' },
  { language: 'en', initiallyFailed: false, format: 'tsv', targetEncoding: 'gb18030' }
]) {
  test(`${format} read settings apply after refresh and failure preserves the last schema (${language}, initial failure=${initiallyFailed}, encoding=${targetEncoding})`, async ({ page }) => {
    const locator = `addp://engine/2/path/sample.${format}?type=file&item_id=8`
    const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
    const item = { id: locator, locator, label: `sample.${format}`, type: 'file', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Encoding engine', type: 'server', hasChildren: true, children: [item] }
    const browserErrors = []
    page.on('pageerror', error => browserErrors.push(error.message))
    const requests = []
    let encoding = 'utf-8'
    let hasHeader = true
    const tableFields = () => [{ name: !hasHeader ? 'Column1' : encoding === targetEncoding ? '城市' : 'old_column', type: 'string', nullable: true }]
    const city = targetEncoding === 'gb18030' ? '城市𠀀😀' : '北京'
    let fail = false
    let releaseRefresh
    const apply = language === 'en' ? 'Apply read settings and refresh' : '应用读取设置并刷新'
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
        hasHeader = body.has_header
        return json(route, { status: 'success' })
      }
      if (path.endsWith('/meta/items/8')) return json(route, {
        item_type: 'file', full_name: `sample.${format}`, attributes: {
          item: { layout: 'single', data_type: 'table', format },
          format_info: { [format]: { encoding } },
          type_info: { table: { native: { has_header: hasHeader }, fields: tableFields() } }
        }
      })
      if (path.endsWith('/manager/preview')) {
        if (initiallyFailed && encoding === 'utf-8') return json(route, { error: 'invalid UTF-8' }, 500)
        const translated = encoding === targetEncoding
        const column = !hasHeader ? 'Column1' : translated ? '城市' : 'old_column'
        return json(route, { preview_type: 'table', data: {
          mode: 'table', columns: [column], rows: [...(!hasHeader ? [{ [column]: '城市' }] : []), { [column]: translated ? city : 'old_value' }], total: hasHeader ? 1 : 2, page: 1, page_size: 20,
          object: { path: `sample.${format}`, content: { kind: format } },
          item_meta: { item_type: 'file', full_name: `sample.${format}`, attributes: [
            { key: 'item', value: { layout: 'single', data_type: 'table', format } },
            { key: 'format_info', value: { [format]: { encoding } } },
            { key: 'type_info', value: { table: { native: { has_header: hasHeader }, fields: tableFields() } } }
          ] }
        } })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    const selector = page.locator('.csv-encoding-select')
    const headerSelector = page.locator('.csv-header-select')
    const firstRowHeader = language === 'en' ? 'First row is header' : '首行为表头'
    const firstRowData = language === 'en' ? 'First row is data' : '首行为数据'
    const button = page.getByRole('button', { name: apply, exact: true })
    if (!initiallyFailed) await expect(page.getByRole('cell', { name: 'old_value', exact: true })).toBeVisible()
    await expect(selector).toContainText('UTF-8')
    await selector.click()
    await page.getByRole('option', { name: targetEncoding.toUpperCase(), exact: true }).click()
    await expect(headerSelector).toContainText(firstRowHeader)
    await headerSelector.click()
    await page.getByRole('option', { name: firstRowData, exact: true }).click()
    expect(requests).toEqual([])
    await button.click()
    await expect.poll(() => requests).toEqual([{ encoding: targetEncoding, has_header: false }])
    if (!initiallyFailed) await expect(page.getByRole('cell', { name: 'old_value', exact: true })).toBeVisible()
    await expect(selector.locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
    await expect(headerSelector.locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
    releaseRefresh()
    await expect(page.getByRole('cell', { name: city, exact: true })).toBeVisible()
    await expect(selector).toContainText(targetEncoding.toUpperCase())
    // The retained Attributes pane has its own three-column field table.
    await page.getByRole('tab', { name: language === 'en' ? 'Attributes' : '属性', exact: true }).click()
    await page.getByRole('tab', { name: language === 'en' ? 'Preview' : '预览', exact: true }).click()
    await expect(page.locator('.el-table__header-wrapper th')).toHaveCount(4)
    await expect(page.locator('.table-preview .el-table__header-wrapper th')).toHaveText(['Column1'])
    await expect(headerSelector).toContainText(firstRowData)
    await expect(page.getByRole('cell', { name: '城市', exact: true })).toBeVisible()
    await page.reload()
    await expect(headerSelector).toContainText(firstRowData)
    await expect(page.getByRole('cell', { name: '城市', exact: true })).toBeVisible()
    fail = true
    await selector.click()
    await page.getByRole('option', { name: 'UTF-8', exact: true }).click()
    await headerSelector.click()
    await page.getByRole('option', { name: firstRowHeader, exact: true }).click()
    await button.click()
    await expect.poll(() => requests.length).toBe(2)
    expect(requests[1]).toEqual({ encoding: 'utf-8', has_header: true })
    await expect(headerSelector).toContainText(firstRowData)
    await expect(selector).toContainText(targetEncoding.toUpperCase())
    await expect(page.getByRole('cell', { name: city, exact: true })).toBeVisible()
    for (const width of [1000, 900]) {
      await page.setViewportSize({ width, height: 800 })
      await expect(button).toBeVisible()
      const box = await button.boundingBox()
      expect(box.x + box.width).toBeLessThanOrEqual(width)
      const layout = await page.locator('.preview-panel .panel-header').evaluate(header => {
        const rect = selector => header.querySelector(selector).getBoundingClientRect().toJSON()
        return {
          title: rect('.header-title'),
          left: rect('.header-left'),
          actions: rect('.panel-actions'),
          encoding: rect('.csv-encoding-select'),
          header: header.getBoundingClientRect().toJSON()
        }
      })
      expect(layout.title.width).toBeGreaterThan(80)
      expect(layout.encoding.width).toBeGreaterThan(100)
      expect(layout.actions.right).toBeLessThanOrEqual(layout.header.right + 1)
      expect(layout.left.right <= layout.actions.left + 1 || layout.left.bottom <= layout.actions.top + 1).toBe(true)
    }
    expect(browserErrors).toEqual([])
  })
}

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
