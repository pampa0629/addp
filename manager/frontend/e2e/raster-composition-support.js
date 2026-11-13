// Shared Manager T3 / real Console T4 oracle; expected grid comes from the
// synthetic fixture formula, never from capability or metadata responses.
export async function rasterCompositionPixels(preview, profile) {
  return preview.evaluate(async (element, profile) => {
    const map = element.__vueParentComponent.setupState.map
    const layer = map.getLayers().getArray().at(-1)
    if (!layer) throw new Error('Rendered GeoTIFF layer is missing')
    const source = layer.getSource()
    const mercator = (lon, lat) => [6378137 * lon * Math.PI / 180, 6378137 * Math.log(Math.tan(Math.PI / 4 + lat * Math.PI / 360))]
    const center = (col, row) => mercator(113 + (col + .5) * .0001, 28 - (row + .5) * .0001)
    const samples = [
      ['mask-center', center(24, 24), profile === 'sidecars'],
      ['mask-left', center(8, 24), true], ['mask-right', center(48, 24), true],
      ['middle', center(128, 128), true], ['bottom', center(192, 224), true],
      ['outside', mercator(112.998, 27.99), false]
    ]
    const pixels = await new Promise(resolve => {
      layer.once('postrender', event => {
        const gl = event.context
        const [width, height] = map.getSize()
        resolve(samples.map(([name, coordinate, opaque]) => {
          const pixel = map.getPixelFromCoordinate(coordinate)
          const x = Math.floor(pixel[0] * gl.drawingBufferWidth / width)
          const y = gl.drawingBufferHeight - 1 - Math.floor(pixel[1] * gl.drawingBufferHeight / height)
          const rgba = new Uint8Array(4)
          const in_canvas = x >= 0 && y >= 0 && x < gl.drawingBufferWidth && y < gl.drawingBufferHeight
          if (in_canvas) gl.readPixels(x, y, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, rgba)
          return { name, opaque, in_canvas, rgba: Array.from(rgba) }
        }))
      })
      map.renderSync()
    })
    return { projection: source.getProjection()?.getCode(), extent: source.getTileGrid()?.getExtent(), pixels }
  }, profile)
}

export function assertRasterCompositionPixels(proof, profile) {
  if (!['msk', 'sidecars'].includes(profile)) throw new Error('Unknown TIFF composition profile')
  const extent = [113, 27.9744, 113.0256, 28]
  if (proof.projection !== 'EPSG:4326' || proof.extent?.length !== 4 || proof.extent.some((value, index) => !Number.isFinite(value) || Math.abs(value - extent[index]) > 1e-8)) throw new Error('TIFF composition rendered CRS or affine extent differs from the fixture grid')
  if (JSON.stringify(proof.pixels?.map(pixel => pixel.name)) !== JSON.stringify(['mask-center', 'mask-left', 'mask-right', 'middle', 'bottom', 'outside'])) throw new Error('TIFF composition rendered samples are incomplete')
  for (const sample of proof.pixels) {
    const opaque = sample.name !== 'outside' && (sample.name !== 'mask-center' || profile === 'sidecars')
    if (!sample.in_canvas || sample.opaque !== opaque || sample.rgba?.length !== 4 || sample.rgba.some(value => !Number.isInteger(value) || value < 0 || value > 255)
      || (opaque ? sample.rgba[3] < 200 : sample.rgba[3] !== 0)) throw new Error(`TIFF composition rendered ${sample.name} ${opaque ? 'visible pixel' : 'transparency'} differs`)
  }
  return proof
}
