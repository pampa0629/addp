import { describe, expect, it } from 'vitest'
import { decodePreviewEWKB } from '../e2e/geometry-preview-support.js'
import { webMercatorPreviewCoordinate } from '../e2e/geometry-preview-coordinate.js'

describe('scope-table geometry HTTP transport', () => {
  it('decodes the Hosted KML Point Z bytes without changing axes or dropping elevation', () => {
    expect(decodePreviewEWKB('AQEAAKDmEAAAH4XrUbg+XEBxPQrXozA8QAAAAAAAACBA')).toEqual({ type: 'Point', coordinates: [112.98, 28.19, 8] })
    expect(decodePreviewEWKB('AQEAAKDmEAAAuB6F61FIXEAUrkfhetQ7QAAAAAAAACJA')).toEqual({ type: 'Point', coordinates: [113.13, 27.83, 9] })
  })
  it('preserves every vertex and its order in a projected LineString', () => {
    const bytes = Buffer.from('010200000002000000000000000000f03f000000000000004000000000000008400000000000001040', 'hex')
    expect(decodePreviewEWKB(bytes.toString('base64'))).toEqual({ type: 'LineString', coordinates: [[1, 2], [3, 4]] })
  })
  it('predicts the display projection round trip exactly without rounding source coordinates', () => {
    const source = [[.5, 0], [0, 10], [-.5, 20], [5, 30], [5, 35], [50, 40]]
    const before = structuredClone(source)
    // Actual doubles from the Hosted map; the EWKB source values remain exact.
    expect(source.map(webMercatorPreviewCoordinate)).toEqual([
      [.5, -7.081154551613622e-10], [0, 10.000000000746313],
      [-.5, 20.000000000584794], [5.000000000000001, 29.999999999490864],
      [5.000000000000001, 34.99999999823251], [50.00000000000001, 39.99999999885617]
    ])
    expect(source).toEqual(before)
    expect(source.map(webMercatorPreviewCoordinate)).not.toEqual(source)
  })
  it('keeps display axis, displacement and vertex order differences observable', () => {
    const expected = [[.5, 0], [10, 0]].map(webMercatorPreviewCoordinate)
    expect([[.5000001, 0], [10, 0]].map(webMercatorPreviewCoordinate)).not.toEqual(expected)
    expect([[0, .5], [0, 10]].map(webMercatorPreviewCoordinate)).not.toEqual(expected)
    expect([[10, 0], [.5, 0]].map(webMercatorPreviewCoordinate)).not.toEqual(expected)
  })
  it.each([null, {}, '', 'not-base64', 'AAAA', 'AQEAAKDmEAAAH4XrUbg+XEBxPQrXozA8QAAAAAAAACB='])('rejects a missing, malformed or truncated cell: %j', value => {
    expect(() => decodePreviewEWKB(value)).toThrow()
  })
})
