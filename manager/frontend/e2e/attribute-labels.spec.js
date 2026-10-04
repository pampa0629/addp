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
          offset_time_original: '+08:00', subsec_time_original: '007',
          exposure_time_seconds: 0.008, f_number: 2.8, exposure_bias_ev: -0.5, focal_length_mm: 35
        }
      }
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg }
      })
      const labels = locale === 'zh-cn'
        ? ['相机厂商', '相机型号', '图像方向', '原始拍摄时间', '拍摄时间时区偏移', '拍摄时间小数秒', '曝光时间（秒）', '光圈 F 值', '曝光补偿（EV）', '焦距（毫米）']
        : ['Camera Make', 'Camera Model', 'Image Orientation', 'Original Capture Time', 'Capture Time Offset', 'Capture Time Subsecond', 'Exposure Time (s)', 'F Number', 'Exposure Bias (EV)', 'Focal Length (mm)']
      for (const label of labels) {
        const field = page.getByText(`EXIF / ${label}`, { exact: true })
        if (status === 'parsed') await expect(field).toBeVisible()
        else await expect(field).toHaveCount(0)
      }
      if (status === 'parsed') {
        await expect(page.getByText('Camera X', { exact: true })).toBeVisible()
        await expect(page.getByText('007', { exact: true })).toBeVisible()
        for (const value of ['0.008', '2.8', '-0.5', '35']) {
          await expect(page.getByText(value, { exact: true })).toBeVisible()
        }
      }
      await expect(page.getByText(locale === 'zh-cn' ? 'EXIF / 图模型' : 'EXIF / Graph Model', { exact: true })).toHaveCount(0)
      const statusLabels = locale === 'zh-cn'
        ? { parsed: '已解析', absent: '未包含 EXIF', invalid: 'EXIF 无效', budget_exceeded: '超出解析预算' }
        : { parsed: 'Parsed', absent: 'No EXIF', invalid: 'Invalid EXIF', budget_exceeded: 'Parse Budget Exceeded' }
      await expect(page.getByText(statusLabels[status], { exact: true })).toBeVisible()
    })
  }

  test(`shows valid exposure fields including zero when EXIF is partially invalid in ${locale}`, async ({ page }) => {
    await openAttributes(page, locale, {
      item: { data_type: 'media', format: 'jpeg', layout: 'single' },
      format_info: { jpeg: { exif_status: 'invalid', exif: { f_number: 4, exposure_bias_ev: 0 } } }
    })
    for (const label of locale === 'zh-cn' ? ['光圈 F 值', '曝光补偿（EV）'] : ['F Number', 'Exposure Bias (EV)']) {
      await expect(page.getByText(`EXIF / ${label}`, { exact: true })).toBeVisible()
    }
    await expect(page.getByText('0', { exact: true })).toBeVisible()
    await expect(page.getByText(locale === 'zh-cn' ? 'EXIF 无效' : 'Invalid EXIF', { exact: true })).toBeVisible()
    await expect(page.getByText(locale === 'zh-cn' ? 'EXIF / 曝光时间（秒）' : 'EXIF / Exposure Time (s)', { exact: true })).toHaveCount(0)
  })

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
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'attributes-e2e', local_account: { username: 'attributes-e2e' } })
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
