import { describe, expect, it } from 'vitest'
import { cameraViewState, preserveDerivedResourceQuery } from '@/utils/s3mViewState'

describe('s3mViewState', () => {
  it('reads Cesium Cartesian3 coordinates without relying on toArray', () => {
    expect(cameraViewState({
      positionWC: { x: 1.25, y: -2.5, z: 3.75 },
      heading: 0.1,
      pitch: -0.2,
      roll: 0.3
    })).toEqual({
      position: [1.25, -2.5, 3.75],
      heading: 0.1,
      pitch: -0.2,
      roll: 0.3
    })
  })

  it('rejects an invalid camera position', () => {
    expect(cameraViewState({ positionWC: { x: 1, y: undefined, z: 3 } })).toBeNull()
  })
})

describe('S3M resource query inheritance', () => {
  it('keeps the result version on every manifest-derived tile request', () => {
    const calls = []
    const resource = {
      getDerivedResource(options) {
        calls.push(options)
        return options
      }
    }

    preserveDerivedResourceQuery(resource)
    expect(resource.getDerivedResource({
      url: './scene/Data/Tile_1/Tile_1.s3m',
      request: 'tile-request'
    })).toEqual({
      url: './scene/Data/Tile_1/Tile_1.s3m',
      request: 'tile-request',
      preserveQueryParameters: true
    })
    expect(calls).toHaveLength(1)
  })
})
