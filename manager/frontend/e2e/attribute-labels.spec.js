import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const locale of ['zh-cn', 'en']) {
  for (const status of ['parsed', 'absent', 'invalid', 'budget_exceeded']) {
    test(`shows JPEG EXIF fields and ${status} diagnostics in ${locale}`, async ({ page }) => {
      const jpeg = { exif_status: status }
      if (status === 'parsed') {
        jpeg.exif = {
          make: 'Camera Vendor', model: 'Camera X', orientation: 6,
          date_time_original: '2026:10:04 12:11:49',
          offset_time_original: '+08:00', subsec_time_original: '007'
        }
      }
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg }
      })
      const labels = locale === 'zh-cn'
        ? ['相机厂商', '相机型号', '图像方向', '原始拍摄时间', '拍摄时间时区偏移', '拍摄时间小数秒']
        : ['Camera Make', 'Camera Model', 'Image Orientation', 'Original Capture Time', 'Capture Time Offset', 'Capture Time Subsecond']
      for (const label of labels) {
        const field = page.getByText(`EXIF / ${label}`, { exact: true })
        if (status === 'parsed') await expect(field).toBeVisible()
        else await expect(field).toHaveCount(0)
      }
      if (status === 'parsed') {
        await expect(page.getByText('Camera X', { exact: true })).toBeVisible()
        await expect(page.getByText('007', { exact: true })).toBeVisible()
      }
      await expect(page.getByText(locale === 'zh-cn' ? 'EXIF / 图模型' : 'EXIF / Graph Model', { exact: true })).toHaveCount(0)
      const statusLabels = locale === 'zh-cn'
        ? { parsed: '已解析', absent: '未包含 EXIF', invalid: 'EXIF 无效', budget_exceeded: '超出解析预算' }
        : { parsed: 'Parsed', absent: 'No EXIF', invalid: 'Invalid EXIF', budget_exceeded: 'Parse Budget Exceeded' }
      await expect(page.getByText(statusLabels[status], { exact: true })).toBeVisible()
    })
  }

  test(`keeps the graph model label scoped to graph facts in ${locale}`, async ({ page }) => {
    await openAttributes(page, locale, {
      item: { data_type: 'graph', format: 'graphml', layout: 'single' },
      type_info: { graph: { model: 'property_graph', directed: true } },
      format_info: { graphml: { model: 'native-format-model' } }
    })
    await expect(page.getByText(locale === 'zh-cn' ? '图模型' : 'Graph Model', { exact: true })).toHaveCount(1)
    await expect(page.getByText('Model', { exact: true })).toBeVisible()
  })
}

async function openAttributes(page, locale, attributes) {
  const locator = 'addp://engine/12/path/sample?type=file&item_id=1201'
  const node = { id: locator, locator, label: 'sample', type: 'file', metadata: { item_id: 1201 } }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'attributes-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'attributes-e2e' })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 12, name: 'Business NFS', engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [node] })
    if (path.endsWith('/meta/items/1201')) return json(route, { id: 1201, item_type: 'file', full_name: 'sample', attributes })
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=attributes`)
}

function json(route, body) {
  return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
}
