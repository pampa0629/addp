// Read the compositor output. The source WebGL buffer may be discarded after drawing.
export async function gribScreenshotColors(page, canvas, swatches) {
  const colors = await swatches.evaluateAll(elements => elements.map(element => getComputedStyle(element).backgroundColor))
  const png = (await canvas.screenshot()).toString('base64')
  return page.evaluate(async ({ png, colors }) => {
    const image = new Image()
    image.src = 'data:image/png;base64,' + png
    await image.decode()
    const copy = document.createElement('canvas')
    copy.width = image.width
    copy.height = image.height
    const context = copy.getContext('2d')
    context.drawImage(image, 0, 0)
    const rgba = context.getImageData(0, 0, copy.width, copy.height).data
    const palette = colors.map(color => color.match(/\d+/g).slice(0, 3).map(Number))
    const counts = palette.map(() => 0)
    for (let offset = 0; offset < rgba.length; offset += 4) {
      const bin = palette.findIndex(rgb => rgb.every((value, channel) => Math.abs(value - rgba[offset + channel]) <= 2))
      if (bin >= 0) counts[bin]++
    }
    return { counts, total: copy.width * copy.height }
  }, { png, colors })
}
