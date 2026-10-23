const chartModules = new WeakMap()

export function trackResourceChart(page) {
  if (chartModules.has(page)) return
  chartModules.set(page, null)
  page.on('request', request => {
    if (/\/echarts_core\.js(?:\?|$)/u.test(request.url())) chartModules.set(page, request.url())
  })
}

export const resourceChartProps = page => page.locator('.chart-renderer').evaluate(element => {
  const { rows, config, maxRows, showValueLabels, valueAxes, zoomable } = element.__vueParentComponent.props
  return { rows, config, maxRows, showValueLabels, valueAxes, zoomable }
})

export const resourceChartZoom = page => {
  const source = chartModules.get(page)
  if (!source) throw new Error('ECharts core request was not observed')
  return page.locator('.chart-renderer').evaluate(async (element, moduleURL) => {
    const { getInstanceByDom } = await import(moduleURL)
    const chart = getInstanceByDom(element), option = chart.getOption()
    const zoom = option.dataZoom?.[0]
    const view = zoom ? chart.getViewOfComponentModel(chart.getModel().getComponent('dataZoom', 0)) : null
    // ECharts includes its move handle in layout; drag the actual painted handle.
    const handles = view ? view._displayables.handles.map(handle => {
      const box = handle.getBoundingRect().clone()
      box.applyTransform(handle.getComputedTransform())
      return { x: box.x + box.width / 2, y: box.y + box.height / 2 }
    }) : []
    return { finished: chart.getZr().animation.isFinished(), handles, start: zoom?.start ?? null, end: zoom?.end ?? null,
      borderColor: zoom?.borderColor, lengths: option.series.map(series => series.data.length) }
  }, source)
}

export async function dragResourceChartZoom(page, fraction = 0.95) {
  await page.locator('.chart-renderer').scrollIntoViewIfNeeded()
  const box = await page.locator('.chart-renderer').boundingBox()
  const [left, right] = (await resourceChartZoom(page)).handles
  const y = box.y + left.y
  await page.mouse.move(box.x + left.x, y)
  await page.mouse.down()
  await page.mouse.move(box.x + left.x + (right.x - left.x) * fraction, y, { steps: 12 })
  await page.mouse.up()
}
