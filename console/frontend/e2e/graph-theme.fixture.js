// Inspect the actual canvas in development and production without Vue debug hooks.
export function graphCanvasHasThemePixels(element) {
  const canvas = element.querySelector('canvas')
  if (!canvas?.width || !canvas.height) return false
  const style = getComputedStyle(element)
  const probe = document.createElement('canvas').getContext('2d')
  probe.fillStyle = style.getPropertyValue('--addp-bg-primary').trim()
  const background = probe.fillStyle
  probe.fillStyle = style.getPropertyValue('--addp-graph-node-stroke').trim()
  probe.fillRect(0, 0, 1, 1)
  const expected = probe.getImageData(0, 0, 1, 1).data
  const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
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
