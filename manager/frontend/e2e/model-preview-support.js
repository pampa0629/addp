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
    // Infer the screenshot background instead of assuming a product theme.
    // Thin grid lines cannot dominate the median of the four corner patches.
    const cornerChannels = [[], [], []]
    const cornerSize = Math.min(8, copy.width, copy.height)
    for (const x of [0, copy.width - cornerSize]) {
      for (const y of [0, copy.height - cornerSize]) {
        for (let dy = 0; dy < cornerSize; dy++) {
          for (let dx = 0; dx < cornerSize; dx++) {
            const offset = ((y + dy) * copy.width + x + dx) * 4
            for (let channel = 0; channel < 3; channel++) cornerChannels[channel].push(rgba[offset + channel])
          }
        }
      }
    }
    const background = cornerChannels.map(values => values.sort((a, b) => a - b)[Math.floor(values.length / 2)])
    const result = { total: copy.width * copy.height, colored: 0, red: 0, green: 0, blue: 0, cyan: 0, magenta: 0, yellow: 0, neutralSolid: 0, checksum: 2166136261 }
    const neutral = new Uint8Array(result.total)
    for (let i = 0; i < rgba.length; i += 4) {
      const [r, g, b] = rgba.subarray(i, i + 3)
      const spread = Math.max(r, g, b) - Math.min(r, g, b)
      if (spread > 60) {
        result.colored++
        if (r > g + 30 && r > b + 30) result.red++
        if (g > r + 30 && g > b + 30) result.green++
        if (b > r + 30 && b > g + 30) result.blue++
        if (g > r + 30 && b > r + 30 && Math.abs(g - b) < 40) result.cyan++
        if (r > g + 30 && b > g + 30 && Math.abs(r - b) < 40) result.magenta++
        if (r > b + 30 && g > b + 30 && Math.abs(r - g) < 40) result.yellow++
      }
      const contrast = Math.abs(r - background[0]) + Math.abs(g - background[1]) + Math.abs(b - background[2])
      neutral[i / 4] = rgba[i + 3] >= 250 && spread <= 40 && contrast >= 64 ? 1 : 0
      result.checksum = Math.imul(result.checksum ^ r ^ (g << 8) ^ (b << 16), 16777619) >>> 0
    }
    // Count only complete opaque 8x8 patches. A gray canvas or a thin grid is
    // not evidence of a rendered gray mesh, even when the grid has high contrast.
    const patchSize = 8
    for (let y = 0; y + patchSize <= copy.height; y += patchSize) {
      for (let x = 0; x + patchSize <= copy.width; x += patchSize) {
        let solid = true
        for (let dy = 0; dy < patchSize && solid; dy++) {
          for (let dx = 0; dx < patchSize; dx++) {
            if (!neutral[(y + dy) * copy.width + x + dx]) {
              solid = false
              break
            }
          }
        }
        if (solid) result.neutralSolid += patchSize * patchSize
      }
    }
    return result
  }, png)
}

// Passive test instrumentation: observe the exact attribute passed to the native
// Blob download without changing the link or the browser filename decision.
export async function installModelDownloadAttributeObserver(page) {
  await page.addInitScript(() => {
    window.__addpModelDownloadAttributes = []
    document.addEventListener('click', event => {
      const link = event.target?.closest?.('a[download]')
      if (link) window.__addpModelDownloadAttributes.push({ name: link.download, blob: link.href.startsWith('blob:') })
    }, true)
  })
}

export async function modelDownloadAttributes(scope) {
  return scope.locator('body').evaluate(body => body.ownerDocument.defaultView.__addpModelDownloadAttributes)
}
