// TIFF page facts belong to Common format metadata, not browser discovery.
export function tiffPages(summary) {
  if (summary?.page_summary_status !== 'parsed' || !Number.isInteger(summary.page_count) ||
    summary.page_count < 1 || summary.page_count > 256 || !Array.isArray(summary.pages) ||
    summary.pages.length !== summary.page_count) return []
  let previous = -1
  for (const page of summary.pages) {
    if (!page || !Number.isInteger(page.ifd_index) || page.ifd_index <= previous || page.ifd_index >= 256 ||
      !Number.isSafeInteger(page.width) || page.width <= 0 ||
      !Number.isSafeInteger(page.height) || page.height <= 0) return []
    previous = page.ifd_index
  }
  return summary.pages
}

export function tiffPreviewSize(width, height) {
  if (!Number.isSafeInteger(width) || width < 1 || !Number.isSafeInteger(height) || height < 1 ||
    width * height > 16_000_000) return null
  const scale = Math.min(1, 1024 / Math.max(width, height))
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
}

export async function readTIFFRGBA(image, width, height, signal) {
  const fd = image.fileDirectory
  const pi = fd.PhotometricInterpretation
  const options = { interleave: true, width, height, signal }
  const bits = Array.from(fd.BitsPerSample || [1])
  const formats = Array.from(fd.SampleFormat || bits.map(() => 1))
  const extras = Array.from(fd.ExtraSamples || [])
  const channels = pi === 2 ? 3 : 1
  let raster
  const direct = [0, 1, 2].includes(pi)
  if (direct) {
    if (bits.some(bit => !(pi === 2 ? [8, 16] : [1, 2, 4, 8, 16]).includes(bit)) || formats.some(format => format !== 1) ||
      bits.length !== channels + extras.length || extras.length > 1 ||
      extras.some(extra => ![1, 2].includes(extra))) throw new Error('unsupportedSamples')
    raster = await image.readRasters(options)
  } else {
    // geotiff.js owns palette and color-space conversion; those outputs are RGB8.
    if (![3, 5, 6, 8].includes(pi) || extras.length || formats.some(format => format !== 1) ||
      (pi !== 3 && bits.some(bit => bit !== 8))) throw new Error('unsupportedSamples')
    raster = await image.readRGB(options)
  }
  const stride = direct ? bits.length : 3
  const out = new Uint8ClampedArray(width * height * 4)
  for (let pixel = 0; pixel < width * height; pixel++) {
    const src = pixel * stride
    const dest = pixel * 4
    const alpha = direct && extras.length ? raster[src + channels] / (2 ** bits[channels] - 1) : 1
    for (let color = 0; color < 3; color++) {
      const channel = channels === 1 ? 0 : color
      let value = direct ? raster[src + channel] / (2 ** bits[channel] - 1) : raster[src + color] / 255
      if (direct && pi === 0) value = 1 - value
      if (direct && extras[0] === 1) value = alpha ? value / alpha : 0
      out[dest + color] = Math.round(value * 255)
    }
    out[dest + 3] = Math.round(alpha * 255)
  }
  return out
}
