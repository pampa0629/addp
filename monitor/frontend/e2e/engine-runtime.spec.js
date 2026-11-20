import { test, expect } from '@playwright/test'
import { engineBackend } from './engine-resources.fixture'

test('search and compute indicators follow the current source without converting missing samples to zero', async ({ page }) => {
  const state = await engineBackend(page)
  const stamp = '2026-10-20T14:00:00.000Z'
  const ids = { meilisearch: '10000000-0000-4000-8000-000000000003', geopython: '10000000-0000-4000-8000-000000000004' }
  const metrics = {
    meilisearch: { tasks_pending: ['tasks', 0], tasks_oldest_wait_seconds: ['seconds', 0], tasks_retained_failed: ['tasks', 76] },
    geopython: { effective_cpu_cores: ['cores', 4], memory_limit_bytes: ['bytes', null], raster_running: ['tasks', 2], raster_waiting: ['tasks', 0], raster_admission_enabled: ['status', 1] }
  }
  await page.route('**/platform/engine_runtime_observations/sources', route => route.fulfill({ json: [{ source: 'geopython', engine_id: 6 }] }))
  const requests = []
  await page.route('**/api/v1/monitor/platform/engine_runtime_observations?**', route => {
    const query = Object.fromEntries(new URL(route.request().url()).searchParams)
    requests.push(query)
    const selected = query.metric ? [query.metric] : Object.keys(metrics[query.source])
    return route.fulfill({ json: {
      source: query.source, id: ids[query.source], started_at: '2026-10-20T13:59:30.000Z',
      discovery_state: 'available', collection: { state: 'collecting', sampled_at: stamp }, queried_at: stamp,
      series: selected.map(metric => {
        const [unit, value] = metrics[query.source][metric]
        const point = (at, value) => ({ evaluated_at: at, sampled_at: value === null ? null : at, value, data_state: value === null ? 'no_data' : 'valid' })
        return { metric_key: metric, unit, window_seconds: 0, dimensions: {},
          points: query.metric ? [point('2026-10-20T13:59:30.000Z', 3), point('2026-10-20T13:59:45.000Z', null), point(stamp, value)] : [point(stamp, value)] }
      })
    } })
  })
  await page.setViewportSize({ width: 1600, height: 980 })
  await page.goto('/engine-resources?refresh=off')
  await expect(page.getByTestId('engine-runtime-value-tasks_pending')).toHaveText(/0/)
  await expect(page.getByTestId('engine-runtime-value-tasks_retained_failed')).toHaveText(/76/)
  const grid = page.getByTestId('engine-list')
  const searchCard = grid.getByTestId('engine-runtime-meilisearch')
  const deploymentCard = grid.getByTestId(/^engine-card-/).first()
  await expect(deploymentCard).toBeVisible()
  const [searchBox, deploymentBox] = await Promise.all([searchCard.boundingBox(), deploymentCard.boundingBox()])
  expect(searchBox.y).toBeCloseTo(deploymentBox.y)
  expect(searchBox.width).toBeCloseTo(deploymentBox.width)
  await page.getByTestId('engine-runtime-meilisearch').getByRole('button', { name: '查看趋势' }).click()
  const dialog = page.getByRole('dialog', { name: 'Meilisearch 索引任务' })
  await expect(dialog.getByTestId('engine-runtime-chart')).toBeVisible()
  expect(requests.at(-1)).toMatchObject({ source: 'meilisearch', id: ids.meilisearch, metric: 'tasks_pending' })
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(searchCard).toBeInViewport()
  expect((await searchCard.boundingBox()).width).toBeLessThan(390)
  await page.getByRole('tab', { name: '计算运行时' }).click()
  await expect(page).toHaveURL(url => url.searchParams.get('scope') === 'runtime')
  await expect(page.getByTestId('engine-runtime-value-memory_limit_bytes')).toHaveText('—')
  await expect(page.getByTestId('engine-runtime-value-raster_running')).toHaveText(/2/)
  await expect(page.getByTestId('engine-runtime-value-raster_admission_enabled')).toHaveText('已开启')
  await expect(page.getByTestId('engine-runtime-meilisearch')).toHaveCount(0)
  expect(state.queries).toEqual([{ scope: 'infra', page: '1', page_size: '20' }])
  await page.getByRole('tab', { name: 'Business 业务引擎' }).click()
  await expect(page.getByTestId('engine-runtime-geopython')).toHaveCount(0)
})
