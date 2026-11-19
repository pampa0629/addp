import { describe, expect, it, vi } from 'vitest'
import { sampleComponentsPolygonPixels } from '../../e2e/online/polygon-components-map.mjs'

function mapFixture({ readError } = {}) {
  const makeLayer = visible => ({
    getVisible: () => visible,
    setVisible: vi.fn(value => { visible = value })
  })
  const basemap = makeLayer(true)
  const hiddenLayer = makeLayer(false)
  const polygonLayer = makeLayer(true)
  const rectangle = { left: 0, top: 0, width: 70, height: 70 }
  const getBoundingClientRect = () => rectangle
  const context = {
    canvas: { width: 70, height: 70, getBoundingClientRect },
    getImageData: vi.fn((left, _top, width, height) => {
      if (readError) throw readError
      const data = new Uint8ClampedArray(width * height * 4)
      // The shared canvas is opaque with the basemap visible, even inside the hole.
      if (basemap.getVisible() || left === 17) {
        for (let i = 3; i < data.length; i += 4) data[i] = 255
      }
      return { data }
    })
  }
  const extent = [20, 0, 26, 6]
  const geometry = { getType: () => 'Polygon', getCoordinates: () => [[], []], getExtent: () => extent }
  Object.assign(polygonLayer, {
    getSource: () => ({ getFeatures: () => Array(8).fill({ getGeometry: () => geometry }) }),
    getOpacity: () => 1,
    getRenderer: () => ({ context })
  })
  const fit = vi.fn()
  const map = {
    getLayers: () => ({ getArray: () => [basemap, polygonLayer, hiddenLayer] }),
    getView: () => ({ getAnimating: () => false, fit }),
    getSize: () => [70, 70],
    getViewport: () => ({ getBoundingClientRect }),
    getPixelFromCoordinate: ([x]) => x === 24 ? [20, 20] : [40, 40],
    renderSync: vi.fn()
  }
  return { element: { __vueParentComponent: { setupState: { mapInstance: map } } }, basemap, hiddenLayer, polygonLayer, map, fit, extent }
}

describe('polygon components pixel probe', () => {
  it('counts only the polygon canvas, leaving the hole transparent and restoring the basemap', () => {
    const { element, basemap, hiddenLayer, polygonLayer, map, fit, extent } = mapFixture()
    expect(sampleComponentsPolygonPixels(element)).toEqual({ filled: 49, hole: 0 })
    expect(basemap.setVisible.mock.calls).toEqual([[false], [true]])
    expect(hiddenLayer.setVisible).not.toHaveBeenCalled()
    expect(polygonLayer.setVisible).not.toHaveBeenCalled()
    expect(fit).toHaveBeenCalledWith(extent, { size: [70, 70], padding: [32, 32, 32, 32], duration: 0 })
    expect(map.renderSync).toHaveBeenCalledTimes(2)
  })

  it('restores visibility when the canvas read fails', () => {
    const { element, basemap, map } = mapFixture({ readError: new Error('canvas unavailable') })
    expect(() => sampleComponentsPolygonPixels(element)).toThrow('canvas unavailable')
    expect(basemap.setVisible.mock.calls).toEqual([[false], [true]])
    expect(map.renderSync).toHaveBeenCalledTimes(2)
  })
})
