import { expect, test } from '@playwright/test'
import { expectColoredModel, modelPixels } from './model-preview-support.js'

async function drawPalette(page, colors, background = '#eeeeee') {
  await page.setContent('<canvas width="400" height="300"></canvas>')
  await page.locator('canvas').evaluate((canvas, { palette, background }) => {
    const context = canvas.getContext('2d')
    context.fillStyle = background
    context.fillRect(0, 0, 400, 300)
    palette.forEach((color, index) => {
      context.fillStyle = color
      context.fillRect(25 + 110 * index, 80, 85, 100)
    })
  }, { palette: colors, background })
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

test('opaque neutral surfaces are visible on light and dark backgrounds', async ({ page }) => {
  for (const background of ['#eeeeee', '#111111']) {
    const canvas = await drawPalette(page, ['#555555', '#999999', '#cccccc'], background)
    const pixels = await modelPixels(page, canvas)
    expect(pixels.neutralSolid / pixels.total).toBeGreaterThan(0.01)
    expect(pixels.neutralSolid / pixels.total).toBeLessThan(0.85)
    expect(pixels.colored).toBe(0)
  }
})

test('a blank neutral canvas is not a rendered solid', async ({ page }) => {
  for (const background of ['#eeeeee', '#111111', '#888888']) {
    const canvas = await drawPalette(page, [], background)
    expect((await modelPixels(page, canvas)).neutralSolid).toBe(0)
  }
})

test('weak and high-contrast thin grids do not become neutral solids', async ({ page }) => {
  for (const background of ['#eeeeee', '#111111']) {
    for (const opacity of [0.22, 1]) {
      const canvas = await drawPalette(page, [], background)
      await canvas.evaluate((element, { background, opacity }) => {
        const context = element.getContext('2d')
        context.globalAlpha = opacity
        context.strokeStyle = background === '#eeeeee' ? '#111111' : '#eeeeee'
        context.lineWidth = 1
        context.beginPath()
        for (let x = 0.5; x < element.width; x += 16) {
          context.moveTo(x, 0)
          context.lineTo(x, element.height)
        }
        for (let y = 0.5; y < element.height; y += 16) {
          context.moveTo(0, y)
          context.lineTo(element.width, y)
        }
        context.stroke()
      }, { background, opacity })
      expect((await modelPixels(page, canvas)).neutralSolid).toBe(0)
    }
  }
})
