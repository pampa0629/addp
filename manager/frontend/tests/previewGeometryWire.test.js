import { describe, expect, it } from 'vitest'
import { decodePreviewEWKB } from '../e2e/geometry-preview-support.js'

describe('scope-table geometry HTTP transport', () => {
  it('decodes the Hosted KML Point Z bytes without changing axes or dropping elevation', () => {
    expect(decodePreviewEWKB('AQEAAKDmEAAAH4XrUbg+XEBxPQrXozA8QAAAAAAAACBA')).toEqual({ type: 'Point', coordinates: [112.98, 28.19, 8] })
    expect(decodePreviewEWKB('AQEAAKDmEAAAuB6F61FIXEAUrkfhetQ7QAAAAAAAACJA')).toEqual({ type: 'Point', coordinates: [113.13, 27.83, 9] })
  })
  it('preserves every vertex and its order in a projected LineString', () => {
    const bytes = Buffer.from('010200000002000000000000000000f03f000000000000004000000000000008400000000000001040', 'hex')
    expect(decodePreviewEWKB(bytes.toString('base64'))).toEqual({ type: 'LineString', coordinates: [[1, 2], [3, 4]] })
  })
  it.each([null, {}, '', 'not-base64', 'AAAA', 'AQEAAKDmEAAAH4XrUbg+XEBxPQrXozA8QAAAAAAAACB='])('rejects a missing, malformed or truncated cell: %j', value => {
    expect(() => decodePreviewEWKB(value)).toThrow()
  })
})
