// Use the caller's Playwright expect so checks belong to the active owner context.
export async function expectColoredModel(page, canvas, expect, minimumPrimaryColors = 2) {
  // Inspect actual screenshot pixels, not metadata or a mocked renderer.
  let pixels
  await expect.poll(async () => {
    pixels = await modelPixels(page, canvas)
    return pixels.colored / pixels.total
  }).toBeGreaterThan(0.01)
  expect(pixels.colored / pixels.total).toBeLessThan(0.85)
  // Orbiting can legitimately hide a face; the initial view exposes all three.
  expect([pixels.red, pixels.green, pixels.blue].filter(count => count > 100).length).toBeGreaterThanOrEqual(minimumPrimaryColors)
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
    const result = { total: copy.width * copy.height, colored: 0, red: 0, green: 0, blue: 0, checksum: 2166136261 }
    for (let i = 0; i < rgba.length; i += 4) {
      const [r, g, b] = rgba.subarray(i, i + 3)
      if (Math.max(r, g, b) - Math.min(r, g, b) > 60) {
        result.colored++
        if (r > g + 30 && r > b + 30) result.red++
        if (g > r + 30 && g > b + 30) result.green++
        if (b > r + 30 && b > g + 30) result.blue++
      }
      result.checksum = Math.imul(result.checksum ^ r ^ (g << 8) ^ (b << 16), 16777619) >>> 0
    }
    return result
  }, png)
}
