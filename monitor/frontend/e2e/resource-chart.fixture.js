const chartModules = new WeakMap()

export function trackResourceChart(page) {
  if (chartModules.has(page)) return
  chartModules.set(page, null)
  page.on('request', request => {
    if (/\/echarts_core\.js(?:\?|$)/u.test(request.url())) chartModules.set(page, request.url())
  })
}

export const resourceChartProps = (page, chart = page.locator('.chart-renderer')) => chart.evaluate(element => {
  const { rows, config, maxRows, showValueLabels, valueAxes, zoomable } = element.__vueParentComponent.props
  return { rows, config, maxRows, showValueLabels, valueAxes, zoomable }
})

export const resourceChartState = (page, pointIndex = null, locator = page.locator('.chart-renderer')) => {
  const source = chartModules.get(page)
  if (!source) throw new Error('ECharts core request was not observed')
  return locator.evaluate(async (element, { moduleURL, pointIndex }) => {
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
    const model = chart.getModel(), legendModel = model.getComponent('legend', 0)
    const legend = chart.getViewOfComponentModel(legendModel).getContentGroup().children().map(item => {
      const name = legendModel.getData()[item.__legendDataIndex].get('name')
      const box = item.getBoundingRect().clone()
      box.applyTransform(item.getComputedTransform())
      return { name, selected: legendModel.isSelected(name), x: box.x + box.width / 2, y: box.y + box.height / 2 }
    })
    const grid = model.getComponent('grid', 0).coordinateSystem.getRect()
    const point = pointIndex === null ? null : { x: chart.convertToPixel({ xAxisIndex: 0 }, pointIndex), y: grid.y + grid.height / 2 }
    return { finished: chart.getZr().animation.isFinished(), axes: option.yAxis.map(axis => ({ name: axis.name, min: axis.min })), handles, start: zoom?.start ?? null, end: zoom?.end ?? null,
      borderColor: zoom?.borderColor, handleRange: view ? [...view._range] : null, lengths: option.series.map(series => series.data.length), legend, point,
      active: model.getSeries().filter(series => !model.isSeriesFiltered(series)).map(series => series.name) }
  }, { moduleURL: source, pointIndex })
}

export async function dragResourceChartZoom(page, fraction = 0.95, chart = page.locator('.chart-renderer')) {
  await chart.scrollIntoViewIfNeeded()
  const box = await chart.boundingBox()
  const [left, right] = (await resourceChartState(page, null, chart)).handles
  const y = box.y + left.y
  await page.mouse.move(box.x + left.x, y)
  await page.mouse.down()
  await page.mouse.move(box.x + left.x + (right.x - left.x) * fraction, y, { steps: 12 })
  await page.mouse.up()
  // Slider input is applied through ECharts' throttled dataZoom dispatch.
  // Wait for the actual model to reach the painted handle range before capture.
  const element = await chart.elementHandle(), frame = await element.ownerFrame()
  const getInstance = await frame.evaluateHandle(async source => (await import(source)).getInstanceByDom, chartModules.get(page))
  try {
    const settled = await frame.waitForFunction(({ element, getInstance }) => {
      const instance = getInstance(element), zoom = instance.getOption().dataZoom[0]
      const view = instance.getViewOfComponentModel(instance.getModel().getComponent('dataZoom', 0))
      return Math.abs(zoom.start - view._range[0]) < 1e-6 && Math.abs(zoom.end - view._range[1]) < 1e-6
    }, { element, getInstance }, { timeout: 5000 })
    await settled.dispose()
  } catch (error) {
    const current = await resourceChartState(page, null, chart)
    error.message += `; slider ${JSON.stringify({ start: current.start, end: current.end, handleRange: current.handleRange })}`
    throw error
  } finally { await getInstance.dispose(); await element.dispose() }
}


export async function clickResourceChartLegend(page, name, chart = page.locator('.chart-renderer')) {
  await chart.scrollIntoViewIfNeeded()
  const box = await chart.boundingBox()
  const item = (await resourceChartState(page, null, chart)).legend.find(entry => entry.name === name)
  if (!item) throw new Error(`Legend item not found: ${name}`)
  await page.mouse.click(box.x + item.x, box.y + item.y)
}

export async function hoverResourceChartPoint(page, pointIndex, chart = page.locator('.chart-renderer')) {
  await chart.scrollIntoViewIfNeeded()
  const box = await chart.boundingBox()
  const { point } = await resourceChartState(page, pointIndex, chart)
  await page.mouse.move(box.x + point.x, box.y + point.y)
}

export const resourceChartTooltip = (page, chart = page.locator('.chart-renderer')) => chart.locator(':scope > div:not(:has(canvas))')
