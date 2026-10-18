import { expect, test } from '@playwright/test'
import { expectColoredModel, modelPixels } from './model-preview-support.js'

async function drawPalette(page, colors) {
  await page.setContent('<canvas width="400" height="300"></canvas>')
  await page.locator('canvas').evaluate((canvas, palette) => {
    const context = canvas.getContext('2d')
    context.fillStyle = '#eeeeee'
    context.fillRect(0, 0, 400, 300)
    palette.forEach((color, index) => {
      context.fillStyle = color
      context.fillRect(25 + 110 * index, 80, 85, 100)
    })
  }, colors)
  return page.locator('canvas')
}

test('RGB initial view requires all three primary colors', async ({ page }) => {
  const canvas = await drawPalette(page, ['#ff0000', '#00ff00', '#0000ff'])
  const pixels = await expectColoredModel(page, canvas, expect, { minimumPrimaryColors: 3 })
  for (const key of ['red', 'green', 'blue']) expect(pixels[key]).toBe(8500)
})

test('orbited palette recognizes real green, magenta and yellow faces', async ({ page }) => {
  const canvas = await drawPalette(page, ['#28cc00', '#85009c', '#8c8c00'])
  const pixels = await expectColoredModel(page, canvas, expect)
  expect(pixels.green).toBe(8500)
  expect(pixels.magenta).toBe(8500)
  expect(pixels.yellow).toBe(8500)
  expect(pixels.red).toBe(0)
  expect(pixels.blue).toBe(0)
})

test('gray palette is never mistaken for colored model faces', async ({ page }) => {
  const canvas = await drawPalette(page, ['#444444', '#888888', '#cccccc'])
  const pixels = await modelPixels(page, canvas)
  for (const key of ['colored', 'red', 'green', 'blue', 'cyan', 'magenta', 'yellow']) expect(pixels[key]).toBe(0)
})
