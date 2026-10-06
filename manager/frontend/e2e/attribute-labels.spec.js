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

  for (const kind of [0, 7]) {
    test(`shows source sensitivity definitions and upper-limit hint for type ${kind} in ${locale}`, async ({ page }) => {
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg: { exif_status: 'parsed', exif: {
          photographic_sensitivity: [200, 65535], sensitivity_type: kind,
          standard_output_sensitivity: 80000, recommended_exposure_index: 102400, iso_speed: 204800
        } } }
      })
      const labels = locale === 'zh-cn'
        ? ['拍摄感光度（源值）', '感光度类型', '标准输出感光度（SOS）', '推荐曝光指数（REI）', 'ISO 感光速度']
        : ['Photographic Sensitivity (Source Values)', 'Sensitivity Type', 'Standard Output Sensitivity (SOS)', 'Recommended Exposure Index (REI)', 'ISO Speed']
      for (const label of labels) await expect(page.getByText(`EXIF / ${label}`, { exact: true })).toBeVisible()
      for (const value of ['200, 65535', '80000', '102400', '204800']) await expect(page.getByText(value, { exact: true })).toBeVisible()
      const typeLabel = kind === 0 ? (locale === 'zh-cn' ? '未知' : 'Unknown')
        : (locale === 'zh-cn' ? 'SOS、REI 和 ISO 感光速度' : 'SOS, REI and ISO Speed')
      await expect(page.getByText(typeLabel, { exact: true })).toBeVisible()
      await expect(page.getByText(`EXIF / ${labels[0]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /65535 表示达到 SHORT 上限/ : /65535 marks the SHORT upper limit/)
    })
  }

  for (const mode of ['2', '3', null]) {
    test(`shows independent GPS quality sources (${mode ?? 'missing mode'}) in ${locale}`, async ({ page }) => {
      const gps = { dop: 0, differential: mode === '3' ? 1 : 0, horizontal_positioning_error_meters: 2.5 }
      if (mode) gps.measure_mode = mode
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg: { exif_status: 'parsed', exif: { gps } } }
      })
      const labels = locale === 'zh-cn'
        ? ['测量方式', '源精度衰减因子（DOP）', '差分修正状态', '源水平定位误差（米）']
        : ['Measurement Mode', 'Source Dilution of Precision (DOP)', 'Differential Correction Status', 'Source Horizontal Positioning Error (m)']
      const modeField = page.getByText(`EXIF / GPS / ${labels[0]}`, { exact: true })
      if (mode) await expect(modeField).toBeVisible()
      else await expect(modeField).toHaveCount(0)
      for (const label of labels.slice(1)) await expect(page.getByText(`EXIF / GPS / ${label}`, { exact: true })).toBeVisible()
      for (const code of ['2', '3']) {
        const value = locale === 'zh-cn' ? (code === '2' ? '二维测量' : '三维测量') : (code === '2' ? '2D Measurement' : '3D Measurement')
        if (mode === code) await expect(page.getByText(value, { exact: true })).toBeVisible()
        else await expect(page.getByText(value, { exact: true })).toHaveCount(0)
      }
      const correction = locale === 'zh-cn'
        ? (gps.differential ? '已应用差分修正' : '未应用差分修正')
        : (gps.differential ? 'Differential Correction Applied' : 'No Differential Correction')
      await expect(page.getByText(correction, { exact: true })).toBeVisible()
      await expect(page.getByText('0', { exact: true })).toBeVisible()
      await expect(page.getByText('2.5', { exact: true })).toBeVisible()
      await expect(page.getByText(`EXIF / GPS / ${labels[1]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /不换算米制误差/ : /no error in meters/)
      await expect(page.getByText(`EXIF / GPS / ${labels[2]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /不推导 RTK/ : /no RTK/)
    })
  }

  test(`shows partial GPS quality without defaulting correction or error in ${locale}`, async ({ page }) => {
    await openAttributes(page, locale, {
      item: { data_type: 'media', format: 'jpeg', layout: 'single' },
      format_info: { jpeg: { exif_status: 'invalid', exif: { gps: { dop: 1.25 } } } }
    })
    await expect(page.getByText(locale === 'zh-cn' ? 'EXIF 无效' : 'Invalid EXIF', { exact: true })).toBeVisible()
    await expect(page.getByText('1.25', { exact: true })).toBeVisible()
    for (const label of locale === 'zh-cn'
      ? ['测量方式', '差分修正状态', '源水平定位误差（米）']
      : ['Measurement Mode', 'Differential Correction Status', 'Source Horizontal Positioning Error (m)']) {
      await expect(page.getByText(`EXIF / GPS / ${label}`, { exact: true })).toHaveCount(0)
    }
  })

  for (const unit of ['K', 'M', 'N', null]) {
    test(`shows independent GPS motion sources (${unit ?? 'missing refs'}) in ${locale}`, async ({ page }) => {
      const gps = { speed: 0, track_degrees: 12.5, image_direction_degrees: 359.99 }
      if (unit) Object.assign(gps, { speed_ref: unit, track_ref: 'T', image_direction_ref: 'M' })
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg: { exif_status: 'parsed', exif: { gps } } }
      })
      const labels = locale === 'zh-cn'
        ? ['源移动速度单位', '源移动速度', '移动方向参考', '移动方向（度）', '图像方向参考', '图像方向（度）']
        : ['Source Speed Unit', 'Source Speed', 'Movement Direction Reference', 'Movement Direction (degrees)', 'Image Direction Reference', 'Image Direction (degrees)']
      for (const [i, key] of ['speed_ref', 'speed', 'track_ref', 'track_degrees', 'image_direction_ref', 'image_direction_degrees'].entries()) {
        const label = page.getByText(`EXIF / GPS / ${labels[i]}`, { exact: true })
        if (key in gps) await expect(label).toBeVisible()
        else await expect(label).toHaveCount(0)
      }
      for (const value of ['0', '12.5', '359.99']) await expect(page.getByText(value, { exact: true })).toBeVisible()
      const units = locale === 'zh-cn' ? { K: '千米/小时', M: '英里/小时', N: '节' }
        : { K: 'Kilometers per Hour', M: 'Miles per Hour', N: 'Knots' }
      for (const [code, label] of Object.entries(units)) {
        if (unit === code) await expect(page.getByText(label, { exact: true })).toBeVisible()
        else await expect(page.getByText(label, { exact: true })).toHaveCount(0)
      }
      for (const label of locale === 'zh-cn' ? ['真北', '磁北'] : ['True North', 'Magnetic North']) {
        if (unit) await expect(page.getByText(label, { exact: true })).toBeVisible()
        else await expect(page.getByText(label, { exact: true })).toHaveCount(0)
      }
      await expect(page.getByText(`EXIF / GPS / ${labels[3]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /与图像方向独立/ : /independent of image direction/)
      await expect(page.getByText(`EXIF / GPS / ${labels[5]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /与移动方向及像素旋转方向独立/ : /independent of movement and pixel rotation/)
    })
  }

  for (const clockCase of ['complete', 'date-only', 'time-only', 'leap', 'invalid']) {
    test(`shows GPS UTC source clock without inventing missing time (${clockCase}) in ${locale}`, async ({ page }) => {
      const gps = { version_id: [2, 3, 0, 0] }
      if (clockCase !== 'time-only' && clockCase !== 'invalid') gps.date_stamp = '2024:02:29'
      if (clockCase !== 'date-only') gps.time_hms = clockCase === 'leap' ? [23, 59, 60] : [0, 0, 0.125]
      if (clockCase === 'complete') gps.date_time_utc = '2024-02-29T00:00:00.125Z'
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        format_info: { jpeg: { exif_status: clockCase === 'invalid' ? 'invalid' : 'parsed', exif: { gps } } }
      })
      const labels = locale === 'zh-cn'
        ? ['源 UTC 日期', '源 UTC 时间（时、分、秒）', 'GPS 接收机时间（UTC）']
        : ['Source UTC Date', 'Source UTC Time (hours, minutes, seconds)', 'GPS Receiver Time (UTC)']
      for (const [i, key] of ['date_stamp', 'time_hms', 'date_time_utc'].entries()) {
        const label = page.getByText(`EXIF / GPS / ${labels[i]}`, { exact: true })
        if (key in gps) await expect(label).toBeVisible()
        else await expect(label).toHaveCount(0)
      }
      if ('time_hms' in gps) await expect(page.getByText(gps.time_hms.join(', '), { exact: true })).toBeVisible()
      if (clockCase === 'complete') {
        await expect(page.getByText(gps.date_time_utc, { exact: true })).toBeVisible()
        await expect(page.getByText(`EXIF / GPS / ${labels[2]}`, { exact: true })).toHaveAttribute('title',
          locale === 'zh-cn' ? /不表示相机快门时间/ : /not the camera shutter time/)
      }
    })
  }

  for (const knownDatum of [true, false]) {
    test(`shows capture GPS facts without inventing a CRS (${knownDatum}) in ${locale}`, async ({ page }) => {
      const point = { latitude: 0, longitude: -120.25 }
      if (knownDatum) Object.assign(point, { datum: 'WGS-84', srid: 4326 })
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'jpeg', layout: 'single' },
        capabilities: { spatial: { capture_location: point } },
        format_info: { jpeg: { exif_status: 'parsed', exif: { gps: {
          version_id: [2, 3, 0, 0], latitude_ref: 'N', latitude_dms: [0, 0, 0],
          longitude_ref: 'W', longitude_dms: [120, 15, 0], altitude_ref: 0,
          altitude_meters: 12.5, status: 'A', ...(knownDatum ? { map_datum: 'WGS-84' } : {})
        } } } }
      })
      const capturePrefix = locale === 'zh-cn' ? '拍摄位置' : 'Capture Location'
      const latitudeLabel = `${capturePrefix} / ${locale === 'zh-cn' ? '拍摄纬度（度）' : 'Capture Latitude (degrees)'}`
      await expect(page.getByText(latitudeLabel, { exact: true })).toBeVisible()
      await expect(page.getByText(latitudeLabel, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /基准未知时不能按 WGS84 定位/ : /unknown datum must not be treated as WGS84/)
      await expect(page.getByText(`${capturePrefix} / ${locale === 'zh-cn' ? '拍摄经度（度）' : 'Capture Longitude (degrees)'}`, { exact: true })).toBeVisible()
      await expect(page.getByText('0', { exact: true })).toBeVisible()
      await expect(page.getByText('-120.25', { exact: true })).toBeVisible()
      const sridLabel = page.getByText(`${capturePrefix} / ${locale === 'zh-cn' ? '拍摄点 SRID' : 'Capture Point SRID'}`, { exact: true })
      if (knownDatum) await expect(sridLabel).toBeVisible()
      else await expect(sridLabel).toHaveCount(0)
      const nativeLabels = locale === 'zh-cn'
        ? ['GPS 版本', '纬度方位', '源纬度（度、分、秒）', '经度方位', '源经度（度、分、秒）', '海拔参考', '源海拔绝对值（米）', '测量状态']
        : ['GPS Version', 'Latitude Reference', 'Source Latitude (degrees, minutes, seconds)', 'Longitude Reference', 'Source Longitude (degrees, minutes, seconds)', 'Altitude Reference', 'Source Absolute Altitude (m)', 'Measurement Status']
      for (const label of nativeLabels) await expect(page.getByText(`EXIF / GPS / ${label}`, { exact: true })).toBeVisible()
      for (const value of locale === 'zh-cn' ? ['北纬', '西经', '海平面以上', '测量中'] : ['North', 'West', 'Above Sea Level', 'Measurement in Progress']) {
        await expect(page.getByText(value, { exact: true })).toBeVisible()
      }
      await expect(page.getByText('2, 3, 0, 0', { exact: true })).toBeVisible()
      await expect(page.getByText(`EXIF / GPS / ${nativeLabels[6]}`, { exact: true })).toHaveAttribute('title',
        locale === 'zh-cn' ? /不表示椭球高/ : /not ellipsoidal height/)
      await expect(page.getByText(locale === 'zh-cn' ? '空间范围' : 'Extent', { exact: true })).toHaveCount(0)
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

for (const locale of ['zh-cn', 'en']) {
  for (const kind of ['gif', 'webp']) {
    test(`shows ${kind} encoded animation timing and diagnostics in ${locale}`, async ({ page }) => {
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: kind, layout: 'single' },
        format_info: { [kind]: { animation: { summary_status: 'parsed', frame_count: 3, duration_ms: 0 } } }
      })
      await expect(page.getByRole('tab', { name: locale === 'zh-cn' ? '属性' : 'Attributes', exact: true })).toHaveAttribute('aria-selected', 'true')
      for (const label of locale === 'zh-cn'
        ? ['动画摘要状态', '图像帧数', '一轮帧延时合计（毫秒）']
        : ['Animation Summary Status', 'Image Frame Count', 'Encoded Frame Delay Sum per Cycle (ms)']) {
        await expect(page.getByText(`${locale === 'zh-cn' ? '动画摘要' : 'Animation Summary'} / ${label}`, { exact: true })).toBeVisible()
      }
      await expect(page.getByText(locale === 'zh-cn' ? '已完整解析' : 'Completely Parsed', { exact: true })).toBeVisible()
      await expect(page.getByText('0', { exact: true })).toBeVisible()
      await expect(page.getByText('3', { exact: true })).toBeVisible()
    })
  }
  for (const [status, zh, en] of [
    ['invalid', '动画结构无效', 'Invalid Animation Structure'],
    ['budget_exceeded', '超出解析预算', 'Parse Budget Exceeded'],
    ['unsupported', '含不支持的渲染或交互控制', 'Unsupported Rendering or Interaction'],
    ['not_animated', '非动画', 'Not Animated']
  ]) {
    test(`shows animation ${status} without inventing frame facts in ${locale}`, async ({ page }) => {
      await openAttributes(page, locale, {
        item: { data_type: 'media', format: 'webp', layout: 'single' },
        format_info: { webp: { animation: { summary_status: status } } }
      })
      await expect(page.getByText(locale === 'zh-cn' ? zh : en, { exact: true })).toBeVisible()
      await expect(page.getByText(locale === 'zh-cn' ? '动画摘要 / 图像帧数' : 'Animation Summary / Image Frame Count', { exact: true })).toHaveCount(0)
    })
  }
}

for (const unavailable of [false, true]) {
  test(`preserves the attributes deep link while metadata is pending (${unavailable ? 'failure' : 'success'})`, async ({ page }) => {
    let releaseMetadata
    let metadataStarted
    const pendingMetadata = new Promise(resolve => { releaseMetadata = resolve })
    const metadataRequest = new Promise(resolve => { metadataStarted = resolve })
    await openAttributes(page, 'zh-cn', {
      item: { data_type: 'media', format: 'jpeg', layout: 'single' },
      format_info: { jpeg: { exif_status: 'budget_exceeded' } }
    }, async (route, body) => {
      metadataStarted()
      await pendingMetadata
      return unavailable
        ? route.fulfill({ status: 404, contentType: 'application/json', body: '{}' })
        : json(route, body)
    })
    await metadataRequest
    // Preview rendering finishes before the independent Meta request does.
    await expect(page.getByText('sample - 数据预览', { exact: true })).toHaveCount(1)
    try {
      await expect(page).toHaveURL(/&tab=attributes$/)
      await expect(page.getByRole('tab', { name: '预览', exact: true })).toHaveAttribute('aria-selected', 'false')
    } finally {
      releaseMetadata()
    }
    if (unavailable) {
      await expect(page.getByRole('tab', { name: '预览', exact: true })).toHaveAttribute('aria-selected', 'true')
      await expect(page).not.toHaveURL(/tab=attributes/)
    } else {
      await expect(page.getByText('超出解析预算', { exact: true })).toBeVisible()
      await expect(page.getByRole('tab', { name: '属性', exact: true })).toHaveAttribute('aria-selected', 'true')
      await expect(page).toHaveURL(/&tab=attributes$/)
    }
  })
}

async function openAttributes(page, locale, attributes, respondMetadata = json) {
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
    if (path.endsWith('/meta/items/1201')) return respondMetadata(route, { id: 1201, item_type: 'file', full_name: 'sample', attributes })
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=attributes`)
}

function json(route, body) {
  return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
}
