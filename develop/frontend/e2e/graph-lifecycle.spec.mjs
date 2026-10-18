import { expect, test } from '@playwright/test'

for (const view of ['schema', 'result']) {
  test(`${view} binds delayed results to the current canvas and clears stale details on refresh`, async ({ page }) => {
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(`/e2e/fixtures/graph-schema-theme.html?view=${view}&empty=1`)
    const canvas = page.locator(view === 'schema' ? '.graph-container canvas' : '.graph-canvas canvas')
    const panel = page.locator(view === 'schema' ? '.detail-panel' : '.props-panel')
    await expect(page.locator('.empty-state')).toBeVisible()
    await expect(canvas).toHaveCount(0)
    await page.getByLabel('主题', { exact: true }).selectOption('purple')
    await page.getByRole('button', { name: '加载数据', exact: true }).click()
    await expect(canvas).toBeVisible()
    const originalCanvas = await canvas.elementHandle()
    const resize = async (width, height) => {
      await page.setViewportSize({ width, height })
      await expect.poll(() => canvas.evaluate(element => ({
        width: parseFloat(element.style.width), height: parseFloat(element.style.height),
        containerWidth: element.parentElement.clientWidth, containerHeight: element.parentElement.clientHeight
      }))).toEqual(expect.objectContaining({ width: width }))
      await expect.poll(() => canvas.evaluate(element => Math.abs(parseFloat(element.style.height) - element.parentElement.clientHeight))).toBeLessThan(2)
    }
    await resize(760, 700)
    await expect(async () => {
      const position = await canvas.evaluate(element => {
        const style = getComputedStyle(element)
        const palette = Array.from({ length: 12 }, (_, i) => style.getPropertyValue(`--addp-graph-category-${i + 1}`).trim().toLowerCase())
        const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
        const bounds = element.getBoundingClientRect()
        for (let i = 0; i < pixels.length; i += 4) {
          const hex = '#' + [...pixels.slice(i, i + 3)].map(value => value.toString(16).padStart(2, '0')).join('')
          if (pixels[i + 3] === 255 && palette.includes(hex)) {
            return { x: (i / 4 % element.width) * bounds.width / element.width, y: Math.floor(i / 4 / element.width) * bounds.height / element.height + 8 }
          }
        }
        return null
      })
      expect(position).not.toBeNull()
      await canvas.click({ position })
      await expect(panel).toBeVisible()
    }).toPass({ timeout: 5_000 })

    await page.getByRole('button', { name: '替换数据', exact: true }).click()
    await expect(canvas).toHaveCount(1)
    await expect(panel).toHaveCount(0)
    expect(await canvas.evaluate((element, original) => element === original, originalCanvas)).toBe(false)
    await resize(1000, 800)
    await page.getByRole('button', { name: '清空数据', exact: true }).click()
    await expect(canvas).toHaveCount(0)
    await expect(page.locator('.empty-state')).toBeVisible()
    await page.getByLabel('主题', { exact: true }).selectOption('light')
    await page.getByRole('button', { name: '加载数据', exact: true }).click()
    await expect(canvas).toHaveCount(1)
    await resize(620, 600)
    await page.getByRole('button', { name: '显示或卸载图', exact: true }).click()
    await expect(canvas).toHaveCount(0)
    await page.getByLabel('主题', { exact: true }).selectOption('dark')
    expect(errors).toEqual([])
  })
}
