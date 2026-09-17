import { test, expect } from '@playwright/test'
import { runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

for (const width of [1280, 800]) {
  test(`published desktop application scrolls with the mouse in fullscreen (${width}px)`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 600 })
    const backend = await installMetricApplicationBackend(context, { rebound: true })
    const published = backend.published
    published.snapshot.page.placements[1] = { component_id: 'component-72', x: 0, y: 12, width: 12, height: 6 }
    await context.route('**/data_applications/*/runtime', route => route.fulfill({ json: published }))
    await page.goto(runtimePath)
    const canvas = page.getByTestId('data-application-canvas')
    await expect(canvas.getByRole('button', { name: '进入全屏', exact: true })).toBeEnabled()
    await page.mouse.move(8, 300)
    await page.mouse.wheel(0, 2000)
    await expect.poll(() => page.evaluate(() => document.scrollingElement.scrollTop)).toBeGreaterThan(0)
    await page.mouse.wheel(0, -4000)
    await expect.poll(() => page.evaluate(() => document.scrollingElement.scrollTop)).toBe(0)

    await canvas.getByRole('button', { name: '进入全屏', exact: true }).click()
    await expect.poll(() => canvas.evaluate(element => document.fullscreenElement === element)).toBe(true)
    await page.mouse.move(8, 300)
    await page.mouse.wheel(0, 2000)
    await expect.poll(() => canvas.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
    await expect(canvas.getByTestId('runtime-component').last().locator('.el-card__header')).toBeInViewport()

    await page.mouse.wheel(0, -4000)
    await expect.poll(() => canvas.evaluate(element => element.scrollTop)).toBe(0)
    await canvas.getByRole('button', { name: '退出全屏', exact: true }).click()
    await expect.poll(() => page.evaluate(() => document.fullscreenElement === null)).toBe(true)
    await page.mouse.move(8, 300)
    await page.mouse.wheel(0, 2000)
    await expect.poll(() => page.evaluate(() => document.scrollingElement.scrollTop)).toBeGreaterThan(0)
    expect(backend.writes).toEqual([])
    expect(backend.unexpected).toEqual([])
  })
}

test('published wallboard keeps its components within the fullscreen viewport', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, { rebound: true })
  const published = backend.published
  published.snapshot.page.display_mode = 'wallboard'
  await context.route('**/data_applications/*/runtime', route => route.fulfill({ json: published }))
  await page.goto(runtimePath)
  const canvas = page.getByTestId('data-application-canvas')
  await canvas.getByRole('button', { name: '进入全屏', exact: true }).click()
  await expect.poll(() => canvas.evaluate(element => document.fullscreenElement === element)).toBe(true)
  await expect.poll(() => canvas.evaluate(element => {
    const bounds = element.getBoundingClientRect()
    return [...element.querySelectorAll('[data-testid="runtime-component"]')].every(card => {
      const rect = card.getBoundingClientRect()
      return rect.top >= bounds.top && rect.bottom <= bounds.bottom
    })
  })).toBe(true)
  await page.mouse.move(8, 300)
  await page.mouse.wheel(0, 2000)
  await expect(canvas.getByRole('button', { name: '退出全屏', exact: true })).toBeInViewport()
  await canvas.getByRole('button', { name: '退出全屏', exact: true }).click()
  await expect.poll(() => page.evaluate(() => document.fullscreenElement === null)).toBe(true)
  expect(backend.unexpected).toEqual([])
})
