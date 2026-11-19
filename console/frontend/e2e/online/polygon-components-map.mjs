export function sampleComponentsPolygonPixels(element) {
  const value = element.__vueParentComponent?.setupState?.mapInstance
  const map = value?.value || value
  if (!map || map.getView().getAnimating()) return null

  const layers = map.getLayers().getArray()
  const layer = layers.find(candidate => candidate.getSource?.()?.getFeatures?.().length === 8)
  if (!layer?.getVisible() || layer.getOpacity() <= 0) return null
  const feature = layer.getSource().getFeatures().find(candidate => {
    const geometry = candidate.getGeometry()
    return geometry.getType() === 'Polygon' && geometry.getCoordinates().length === 2
  })
  if (!feature) throw new Error('polygon components hole feature missing')

  // OpenLayers can reuse the previous layer's canvas, including opaque basemap pixels.
  const otherLayers = layers.filter(candidate => candidate !== layer && candidate.getVisible())
  try {
    for (const other of otherLayers) other.setVisible(false)
    map.getView().fit(feature.getGeometry().getExtent(), { size: map.getSize(), padding: [32, 32, 32, 32], duration: 0 })
    map.renderSync()

    const context = layer.getRenderer()?.context
    if (!context) return null
    const viewport = map.getViewport().getBoundingClientRect()
    const rectangle = context.canvas.getBoundingClientRect()
    return Object.fromEntries([['filled', [24, 0.5]], ['hole', [23, 3]]].map(([key, coordinate]) => {
      const pixel = map.getPixelFromCoordinate(coordinate)
      const x = Math.round((viewport.left + pixel[0] - rectangle.left) * context.canvas.width / rectangle.width)
      const y = Math.round((viewport.top + pixel[1] - rectangle.top) * context.canvas.height / rectangle.height)
      if (x < 3 || y < 3 || x + 4 > context.canvas.width || y + 4 > context.canvas.height) return [key, null]
      const data = context.getImageData(x - 3, y - 3, 7, 7).data
      let count = 0
      for (let i = 3; i < data.length; i += 4) if (data[i] > 0) count++
      return [key, count]
    }))
  } finally {
    for (const other of otherLayers) other.setVisible(true)
    map.renderSync()
  }
}
