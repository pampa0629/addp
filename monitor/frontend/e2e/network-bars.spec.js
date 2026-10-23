import { expect, test } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { node, resourceBackend } from './node-resources.fixture'

for (const locale of ['zh-cn', 'en']) test(`network grouped bars retain the full rate grid and independent missing values (${locale})`, async ({ page }) => {
  const keys = ['node.network.receive_bytes_per_second', 'node.network.transmit_bytes_per_second']
  let mode = ''
  const state = await resourceBackend(page, { locale, trendResponse(body, params) {
    const start = Date.parse(params.get('start')), end = Date.parse(params.get('end'))
    const step = Math.max(15, Math.ceil((end - start) / 1000 / 999 / 15) * 15)
    body.step_seconds = step
    for (const line of body.series) line.points = Array.from({ length: Math.floor((end - start) / (step * 1000)) + 1 }, (_, index) => {
      const stamp = new Date(start + index * step * 1000).toISOString()
      const receive = line.metric_key === keys[0]
      const data_state = mode === 'gaps' && receive && index === 1 ? 'no_data' : mode === 'gaps' && !receive && index === 2 ? 'stale' : 'valid'
      return { evaluated_at: stamp, sampled_at: data_state === 'no_data' ? null : data_state === 'stale' ? new Date(Date.parse(stamp) - 61000).toISOString() : stamp,
        value: data_state === 'no_data' ? null : data_state === 'stale' ? 1e15 : (receive ? index === 0 ? 0 : 1 : 2) * 1024 ** 2, data_state }
    })
    body.series.reverse()
    if (mode === 'foreign') body.series[0].dimensions.device = 'another-interface'
  } })
  const chartProps = () => page.locator('.chart-renderer').evaluate(element => {
    const { rows, config, maxRows, showValueLabels, valueAxes } = element.__vueParentComponent.props
    return { rows, config, maxRows, showValueLabels, valueAxes }
  })
  const zoomState = () => page.locator('.chart-renderer').evaluate(async element => {
    const source = performance.getEntriesByType('resource').find(entry => /\/echarts_core\.js(?:\?|$)/u.test(entry.name))
    if (!source) throw new Error('ECharts core module was not loaded')
    const { getInstanceByDom } = await import(source.name)
    const chart = getInstanceByDom(element)
    const option = chart.getOption()
    // Use painted handle bounds: ECharts includes the move handle in slider layout.
    const view = chart.getViewOfComponentModel(chart.getModel().getComponent('dataZoom', 0))
    const handles = view._displayables.handles.map(handle => {
      const box = handle.getBoundingRect().clone()
      box.applyTransform(handle.getComputedTransform())
      return { x: box.x + box.width / 2, y: box.y + box.height / 2 }
    })
    return { handles, start: option.dataZoom[0].start, borderColor: option.dataZoom[0].borderColor, lengths: option.series.map(series => series.data.length) }
  })
  let savedZoom
  const refresh = () => page.getByRole('button', { name: locale === 'en' ? 'Refresh' : '刷新', exact: true }).click()
  await page.setViewportSize({ width: 620, height: 900 })
  for (const [range, count, step] of [['5m', 21, 15], ['1h', 241, 15], ['24h', 961, 90], ['7d', 984, 615]]) {
    const before = state.reads.filter(row => row.path.endsWith('resource_trends')).length
    await page.goto(`/node-resources/${node}?metric=${keys[0]}&device=eth0&range=${range}&refresh=off`)
    await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
    await expect(page.getByTestId('resource-paired-hint')).toContainText(locale === 'en' ? 'grouped bars' : '并排柱状')
    await expect(page.getByTestId('resource-paired-hint')).toContainText('MiB/s')
    await expect(page.getByTestId('resource-trend-networkReceive')).toContainText('1.00 MiB/s')
    await expect(page.getByTestId('resource-trend-networkTransmit')).toContainText('2.00 MiB/s')
    const props = await chartProps()
    expect(props.config.chart_type).toBe('bar')
    expect(props.maxRows).toBe(1000)
    expect(props.showValueLabels).toBe(false)
    expect(props.rows).toHaveLength(count)
    expect(props.rows[0]).toMatchObject({ networkReceive: 0, networkTransmit: 2 })
    expect(props.rows.at(-1)).toMatchObject({ networkReceive: 1, networkTransmit: 2 })
    expect(props.config.field_presentations.slice(1).map(item => item.unit)).toEqual(['MiB/s', 'MiB/s'])
    expect(props.valueAxes).toEqual([{ measures: ['networkReceive', 'networkTransmit'], label: locale === 'en' ? 'Network receive / transmit rate' : '网卡收发速率', min: 0 }])
    expect((Date.parse(props.rows.at(-1).evaluated_at) - Date.parse(props.rows[0].evaluated_at)) / (count - 1)).toBe(step * 1000)
    const reads = state.reads.filter(row => row.path.endsWith('resource_trends'))
    expect(reads).toHaveLength(before + 1)
    expect(reads.at(-1).query).toMatchObject({ metrics: keys.join(','), device: 'eth0' })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (range === '5m' || range === '7d') {
      const screenshot = join(mkdtempSync(join(tmpdir(), 'addp-network-bars-')), `${locale}-${range}.png`)
      await page.getByTestId('resource-chart').screenshot({ path: screenshot, animations: 'disabled' })
      console.log(`Network bars screenshot: ${screenshot}`)
    }
    if (range === '7d') {
      const box = await page.locator('.chart-renderer').boundingBox()
      const [left, right] = (await zoomState()).handles
      const y = box.y + left.y
      await page.mouse.move(box.x + left.x, y)
      await page.mouse.down()
      await page.mouse.move(box.x + left.x + (right.x - left.x) * 0.95, y, { steps: 12 })
      await page.mouse.up()
      await expect.poll(async () => (await zoomState()).start).toBeGreaterThan(90)
      savedZoom = (await zoomState()).start
      expect((await zoomState()).lengths).toEqual([984, 984])
      expect(state.reads.filter(row => row.path.endsWith('resource_trends'))).toHaveLength(before + 1)
      await page.evaluate(() => document.documentElement.style.setProperty('--addp-border-color', 'rgb(20, 30, 40)'))
      await expect.poll(async () => (await zoomState()).borderColor).toBe('rgb(20, 30, 40)')
      expect((await zoomState()).start).toBeCloseTo(savedZoom, 2)
      const screenshot = join(mkdtempSync(join(tmpdir(), 'addp-network-bars-')), `${locale}-7d-zoom.png`)
      await page.getByTestId('resource-chart').screenshot({ path: screenshot, animations: 'disabled' })
      console.log(`Network bars screenshot: ${screenshot}`)
    }
  }
  mode = 'gaps'
  await refresh()
  await expect.poll(async () => (await chartProps()).rows[1]).toMatchObject({ networkReceive: null, networkTransmit: 2 })
  expect((await chartProps()).rows[2]).toMatchObject({ networkReceive: 1, networkTransmit: null })
  expect((await chartProps()).config.field_presentations[1].unit).toBe('MiB/s')
  expect((await zoomState()).start).toBeCloseTo(savedZoom, 2)
  await page.getByTestId('resource-metric').click()
  await page.getByRole('option', { name: locale === 'en' ? 'Network transmit throughput' : '网络发送吞吐', exact: true }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('metric') === keys[1] && url.searchParams.get('device') === 'eth0' && url.searchParams.get('range') === '7d')
  await page.reload()
  await page.getByText(locale === 'en' ? 'View sample details' : '查看采样明细', { exact: true }).click()
  await expect(page.getByTestId('resource-evidence')).toContainText('2.00 MiB/s')
  mode = 'foreign'
  await refresh()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  await expect(page.getByTestId('resource-memoryTotal')).toContainText('16.00 GiB')
  mode = ''
  await refresh()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
  expect((await zoomState()).start).toBe(0)
  state.trendMode = 'budget'
  await refresh()
  await expect(page.getByTestId('resource-trend-error')).toBeVisible()
  await expect(page.getByTestId('resource-chart')).toHaveCount(0)
  state.trendMode = ''
  await refresh()
  await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
})
