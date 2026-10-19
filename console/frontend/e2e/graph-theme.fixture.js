// Inspect the actual canvas in development and production without Vue debug hooks.
export function graphCanvasHasThemePixels(element) {
  const canvas = element.querySelector('canvas')
  if (!canvas?.width || !canvas.height) return false
  const style = getComputedStyle(element)
  const probe = document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  probe.fillStyle = style.getPropertyValue('--addp-bg-primary').trim()
  const background = probe.fillStyle
  probe.fillStyle = style.getPropertyValue('--addp-graph-node-stroke').trim()
  probe.fillRect(0, 0, 1, 1)
  const expected = probe.getImageData(0, 0, 1, 1).data
  const readback = document.createElement('canvas')
  readback.width = canvas.width
  readback.height = canvas.height
  const reader = readback.getContext('2d', { willReadFrequently: true })
  reader.drawImage(canvas, 0, 0)
  const pixels = reader.getImageData(0, 0, readback.width, readback.height).data
  const backgroundProbe = document.createElement('span')
  backgroundProbe.style.color = background
  element.append(backgroundProbe)
  const backgroundMatches = style.backgroundColor === getComputedStyle(backgroundProbe).color
  backgroundProbe.remove()
  if (!backgroundMatches) return false
  for (let offset = 0; offset < pixels.length; offset += 4) {
    if (pixels[offset + 3] > 200 && [0, 1, 2].every(channel => Math.abs(pixels[offset + channel] - expected[channel]) <= 4)) return true
  }
  return false
}

export function graphDocumentHasThemeTypography(element) {
  const probe = document.createElement('span')
  probe.style.fontFamily = 'var(--el-font-family)'
  probe.style.fontSize = 'var(--el-font-size-base)'
  element.append(probe)
  const actual = getComputedStyle(element)
  const expected = getComputedStyle(probe)
  const matches = actual.fontFamily === expected.fontFamily && actual.fontSize === expected.fontSize
  probe.remove()
  return matches
}

export function graphBrowserDiagnostic(message, stage, secrets = []) {
  let text = message.text()
  for (const value of [...secrets].filter(value => typeof value === 'string' && value).sort((a, b) => b.length - a.length)) {
    text = text.replaceAll(value, '[redacted]').replaceAll(encodeURIComponent(value), '[redacted]')
  }
  text = text.replace(/Bearer\s+[^\s"',;]+/gi, 'Bearer [redacted]')
    .replace(/\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b/g, '[redacted]')
    .replace(/https?:\/\/[^\s)<>]+/g, value => {
      try { const url = new URL(value); return url.origin + url.pathname } catch { return '[url]' }
    })
  const location = message.location()
  let source = ''
  try { source = new URL(location.url).pathname } catch { /* browser-generated messages can omit a URL */ }
  return {
    stage, type: message.type(), message: text.slice(0, 4000), source,
    line: location.lineNumber ?? null, column: location.columnNumber ?? null
  }
}
