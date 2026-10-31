// Acceptance oracles shared by the controlled Console regression and the real
// manual product scene. Read HTTP responses and public DOM only; no Vue state,
// ECharts internals, token injection or production formatter imports.
const keys = ['http.requests.per_second', 'http.failures.percent', 'http.client_errors.percent',
  'http.duration.mean_seconds', 'http.duration.p95_seconds', 'http.duration.p99_seconds', 'http.active_requests']
const number = text => text === '—' || !text.trim() ? null : Number(text.replaceAll(',', '').trim())
function agrees(text, point, integer = false) {
  if (!point || point.data_state !== 'valid' || point.value === null) return text.trim() === '—'
  const displayed = number(text)
  return displayed !== null && Number.isFinite(displayed) && Math.abs(displayed - point.value) <= (integer ? 0.5 : 0.000500001)
}
function values(observation, catalog = keys) {
  return catalog.map(key => observation?.series.find(series => series.metric_key === key)?.points[0])
}
export function httpResponses(page, action, { owner, detail = true } = {}) {
  const endpoint = kind => page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/monitor/platform/' + kind && response.ok() &&
      (!owner || url.searchParams.get('instance_ids') === String(owner.id))
  }).then(response => response.json())
  const pending = [endpoint('http_observations'), ...(detail ? [endpoint('http_interfaces'), endpoint('http_trends')] : [])]
  for (const promise of pending) promise.catch(() => {})
  return Promise.resolve().then(action).then(() => Promise.all(pending))
}
export async function assertHTTPValues(expect, cells, points) {
  await expect(cells).toHaveCount(points.length)
  for (const [index, point] of points.entries()) {
    await expect.poll(async () => agrees(await cells.nth(index).innerText(), point, index === 6)).toBe(true)
  }
}
function ownRow(expect, payload, owner, stoppedAt) {
  expect(payload.data).toHaveLength(1)
  const row = payload.data[0]
  expect(row.subject).toMatchObject(Object.fromEntries(['id', 'module_name', 'instance_id', 'role'].map(key => [key, owner[key]])))
  expect(row.collection.state).toBe('collecting')
  if (stoppedAt) {
    expect(Date.parse(row.collection.sampled_at)).toBeGreaterThanOrEqual(Date.parse(stoppedAt))
    expect(Date.parse(row.collection.sampled_at)).toBeLessThanOrEqual(Date.now())
  }
  return row
}
export async function assertHTTPPage(expect, frame, owner, [summary, ranking, trend], { sort = 'mean', stoppedAt } = {}) {
  await expect(frame.getByTestId('http-error')).toHaveCount(0)
  await expect(frame.getByTestId('http-detail-error')).toHaveCount(0)
  const subject = ownRow(expect, summary, owner, stoppedAt)
  const rows = frame.getByTestId('http-instances').locator('tbody tr')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText(owner.instance_id)
  await expect(rows.first()).toContainText(owner.module_name)
  await assertHTTPValues(expect, rows.first().locator('td:nth-child(n+4):nth-child(-n+10)'), values(subject.observations[0]))
  const ranked = ownRow(expect, ranking, owner, stoppedAt).observations
  expect(ranked.length).toBeGreaterThan(0)
  const rankKey = sort === 'qps' ? keys[0] : keys[3]
  const value = row => row.series.find(series => series.metric_key === rankKey)?.points[0]
  const score = row => value(row)?.data_state === 'valid' ? value(row).value : -Infinity
  const ordered = [...ranked].sort((a, b) => score(b) - score(a) || a.route.localeCompare(b.route) || a.method.localeCompare(b.method))
  const interfaces = frame.getByTestId('http-interfaces').locator('tbody tr')
  await expect(interfaces).toHaveCount(ordered.length)
  for (const [index, row] of ordered.entries()) {
    await expect(interfaces.nth(index).locator('td').first()).toHaveText(row.method + ' ' + (row.route || '未匹配路由'))
    await assertHTTPValues(expect, interfaces.nth(index).locator('td:nth-child(n+2)'), values(row, keys.slice(0, -1)))
  }
  const history = ownRow(expect, trend, owner, stoppedAt)
  const points = history.observations[0]?.series.find(series => series.metric_key === keys[0])?.points || []
  const valid = points.filter(point => point.data_state === 'valid' && Number.isFinite(point.value))
  expect(valid.length).toBeGreaterThan(0)
  const chart = frame.getByTestId('http-trend'), canvas = chart.locator('canvas')
  await expect(canvas).toBeVisible()
  // Axis tooltip is rendered by the real chart. Move within the recent part
  // of the window; accept a shown value only if it occurs in this response.
  await expect.poll(async () => {
    const box = await canvas.boundingBox()
    if (!box) return false
    for (const fraction of [0.9, 0.8, 0.7, 0.5]) {
      await canvas.hover({ position: { x: box.width * fraction, y: box.height / 2 } })
      const tooltip = chart.locator('.chart-renderer > div[style*="position: absolute"]').last()
      if (await tooltip.isVisible() && await tooltip.locator('span').count()) {
        const text = await tooltip.locator('span').last().innerText()
        if (valid.some(point => agrees(text, point))) return true
      }
    }
    return false
  }).toBe(true)
}

export async function assertHTTPRestored(expect, page, frame, query) {
  await expect.poll(() => Object.fromEntries(new URL(page.url()).searchParams)).toEqual(query)
  const iframe = page.locator('iframe[data-testid="module-iframe"]')
  await expect(iframe).toHaveCount(1)
  await expect.poll(async () => {
    const child = page.frames().find(candidate => candidate.parentFrame() === page.mainFrame())
    return child ? Object.fromEntries(new URL(child.url()).searchParams) : null
  }).toEqual(query)
  await expect(frame.locator('input[aria-label="服务模块"]')).toHaveValue(query.module_name)
  for (const [label, value] of [['观测层', '后端处理'], ['统计窗口', '1 分钟'], ['排行依据', query.sort === 'qps' ? 'QPS' : '平均耗时'], ['趋势时段', '近 5 分钟']]) {
    await expect(frame.locator('.el-select').filter({ has: frame.locator('[aria-label="' + label + '"]') })).toContainText(value)
  }
}

export async function assertHTTPStoppedPage(expect, frame, owner) {
  await expect(frame.getByTestId('http-error')).toHaveCount(0)
  await expect(frame.getByTestId('http-instances')).not.toContainText(owner.instance_id)
  await expect(frame.getByTestId('http-instances').locator('tbody tr')).toHaveCount(0)
  await expect(frame.getByTestId('http-interfaces').locator('tbody tr')).toHaveCount(0)
  await expect(frame.getByTestId('http-trend')).toHaveCount(0)
  await expect(frame.getByTestId('http-detail-error')).toContainText('请从列表重新选择')
}
