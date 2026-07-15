import { describe, expect, it } from 'vitest'
import {
  cameraViewState,
  isThreeS3MViewState,
  isS3MViewStateForMode,
  normalizeS3MViewMode,
  preserveDerivedResourceQuery,
  S3M_VIEW_MODE_GLOBE,
  S3M_VIEW_MODE_MODEL,
  S3M_THREE_RENDERER_RUNTIME,
  s3mGlobeCameraRange,
  s3mRootBoundingSphere
} from '@/utils/s3mViewState'

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

describe('S3M root bounds', () => {
  it('converts oriented boxes before merging root bounding spheres', () => {
    const calls = []
    const Cesium = {
      BoundingSphere: {
        clone(volume) {
          calls.push(['clone', volume])
          return { kind: 'sphere', volume }
        },
        fromOrientedBoundingBox(volume) {
          calls.push(['obb', volume])
          return { kind: 'obb', volume }
        },
        fromBoundingSpheres(spheres) {
          calls.push(['merge', spheres])
          return { kind: 'merged', spheres }
        }
      }
    }
    const sphereVolume = { radius: 12 }
    const orientedVolume = { halfAxes: {} }
    const result = s3mRootBoundingSphere(Cesium, [
      { boundingVolume: { boundingVolume: sphereVolume } },
      { boundingVolume: { boundingVolume: orientedVolume } }
    ])

    expect(result.kind).toBe('merged')
    expect(result.spheres.map(item => item.kind)).toEqual(['sphere', 'obb'])
    expect(calls.map(item => item[0])).toEqual(['clone', 'obb', 'merge'])
  })
})

describe('S3M preview view mode', () => {
  it('accepts globe mode and defaults every other value to model mode', () => {
    expect(normalizeS3MViewMode(S3M_VIEW_MODE_GLOBE)).toBe(S3M_VIEW_MODE_GLOBE)
    expect(normalizeS3MViewMode('earth')).toBe(S3M_VIEW_MODE_MODEL)
    expect(normalizeS3MViewMode(undefined)).toBe(S3M_VIEW_MODE_MODEL)
  })

  it('restores camera state only when it declares the active mode', () => {
    expect(isS3MViewStateForMode({ view_mode: 'model' }, 'model')).toBe(true)
    expect(isS3MViewStateForMode({ view_mode: 'globe' }, 'model')).toBe(false)
    expect(isS3MViewStateForMode({ position: [1, 2, 3] }, 'model')).toBe(false)
  })

  it('keeps enough globe context while scaling for large scenes', () => {
    expect(s3mGlobeCameraRange(100)).toBe(5000)
    expect(s3mGlobeCameraRange(10000)).toBe(50000)
    expect(s3mGlobeCameraRange(undefined)).toBe(5000)
  })
})

describe('Three.js S3M camera state', () => {
  it('restores only state written by the Three.js S3M runtime', () => {
    expect(isThreeS3MViewState({ renderer_runtime: S3M_THREE_RENDERER_RUNTIME })).toBe(true)
    expect(isThreeS3MViewState({ view_mode: 'model' })).toBe(false)
    expect(isThreeS3MViewState({ position: [1, 2, 3] })).toBe(false)
  })

})
