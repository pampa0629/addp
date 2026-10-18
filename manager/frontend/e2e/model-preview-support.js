// Use the caller's Playwright expect so checks belong to the active owner context.
export async function expectColoredModel(page, canvas, expect, { minimumPrimaryColors = 0 } = {}) {
  // Inspect actual screenshot pixels, not metadata or a mocked renderer.
  let pixels
  await expect.poll(async () => {
    pixels = await modelPixels(page, canvas)
    return pixels.colored / pixels.total
  }).toBeGreaterThan(0.01)
  expect(pixels.colored / pixels.total).toBeLessThan(0.85)
  const primaryColors = [pixels.red, pixels.green, pixels.blue]
  expect(primaryColors.filter(count => count > 100).length).toBeGreaterThanOrEqual(minimumPrimaryColors)
  // The cube also has CMY faces. Orbiting can expose those instead of RGB.
  expect([...primaryColors, pixels.cyan, pixels.magenta, pixels.yellow].filter(count => count > 100).length).toBeGreaterThanOrEqual(2)
  return pixels
}

export async function modelPixels(page, canvas) {
  const png = (await canvas.screenshot()).toString('base64')
  return page.evaluate(async base64 => {
    const image = new Image()
    image.src = `data:image/png;base64,${base64}`
    await image.decode()
    const copy = document.createElement('canvas')
    copy.width = image.width
    copy.height = image.height
    const context = copy.getContext('2d')
    context.drawImage(image, 0, 0)
    const rgba = context.getImageData(0, 0, copy.width, copy.height).data
    const result = { total: copy.width * copy.height, colored: 0, red: 0, green: 0, blue: 0, cyan: 0, magenta: 0, yellow: 0, checksum: 2166136261 }
    for (let i = 0; i < rgba.length; i += 4) {
      const [r, g, b] = rgba.subarray(i, i + 3)
      if (Math.max(r, g, b) - Math.min(r, g, b) > 60) {
        result.colored++
        if (r > g + 30 && r > b + 30) result.red++
        if (g > r + 30 && g > b + 30) result.green++
        if (b > r + 30 && b > g + 30) result.blue++
        if (g > r + 30 && b > r + 30 && Math.abs(g - b) < 40) result.cyan++
        if (r > g + 30 && b > g + 30 && Math.abs(r - b) < 40) result.magenta++
        if (r > b + 30 && g > b + 30 && Math.abs(r - g) < 40) result.yellow++
      }
      result.checksum = Math.imul(result.checksum ^ r ^ (g << 8) ^ (b << 16), 16777619) >>> 0
    }
    return result
  }, png)
}
