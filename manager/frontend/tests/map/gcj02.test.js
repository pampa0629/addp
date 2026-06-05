import { describe, expect, it } from 'vitest'
import {
  gcj02ToWGS84,
  mapDisplayCoordinate,
  mapSourceCoordinate,
  wgs84ToGCJ02
} from '../../../../common-frontend/map/src/utils/gcj02.js'

const GCJ02_PROFILE = { coordinate_policy: 'gcj02' }
const WGS84_PROFILE = { coordinate_policy: 'wgs84' }

describe('gcj02 display adapter', () => {
  it('shifts coordinates inside China for GCJ-02 basemaps', () => {
    const beijing = [116.397128, 39.916527]
    const shifted = wgs84ToGCJ02(beijing)

    expect(Math.abs(shifted[0] - beijing[0])).toBeGreaterThan(0.001)
    expect(Math.abs(shifted[1] - beijing[1])).toBeGreaterThan(0.001)
  })

  it('does not shift coordinates outside China', () => {
    const london = [-0.1276, 51.5072]

    expect(wgs84ToGCJ02(london)).toEqual(london)
    expect(mapDisplayCoordinate(london, GCJ02_PROFILE)).toEqual(london)
  })

  it('keeps display/source coordinate conversion reversible enough for preview interactions', () => {
    const source = [116.397128, 39.916527]
    const display = mapDisplayCoordinate(source, GCJ02_PROFILE)
    const restored = mapSourceCoordinate(display, GCJ02_PROFILE)

    expect(restored[0]).toBeCloseTo(source[0], 4)
    expect(restored[1]).toBeCloseTo(source[1], 4)
    expect(gcj02ToWGS84(display)[0]).toBeCloseTo(source[0], 4)
    expect(mapDisplayCoordinate(source, WGS84_PROFILE)).toEqual(source)
  })
})
