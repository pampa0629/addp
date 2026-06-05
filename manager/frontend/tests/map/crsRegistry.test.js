import { describe, expect, it } from 'vitest'
import {
  crsSuppressionStatus,
  getPreviewCRSTransform,
  sourceCRSFromPreview,
  sourceSRIDFromPreview,
  transformGeoJSONGeometryToWGS84
} from '../../../../common-frontend/map/src/utils/crsRegistry.js'

describe('preview CRS registry', () => {
  it('treats EPSG:4326 preview geometry as direct WGS84 data', () => {
    const transform = getPreviewCRSTransform({ source_srid: 4326 })

    expect(transform.status).toBe('direct')
    expect(crsSuppressionStatus(transform)).toBe('')
  })

  it('detects source CRS from nested preview metadata', () => {
    const preview = {
      object: {
        content: {
          metadata: {
            source_crs: 'EPSG:3857'
          }
        }
      }
    }

    expect(sourceSRIDFromPreview(preview)).toBe(3857)
    expect(sourceCRSFromPreview(preview)).toBe('EPSG:3857')
  })

  it('transforms built-in EPSG:3857 preview geometry to WGS84', () => {
    const transform = getPreviewCRSTransform({ source_srid: 3857 })
    const geometry = transformGeoJSONGeometryToWGS84({
      type: 'Point',
      coordinates: [1113194.9079327357, 0]
    }, transform)

    expect(transform.status).toBe('transformable')
    expect(geometry.coordinates[0]).toBeCloseTo(10, 6)
    expect(geometry.coordinates[1]).toBeCloseTo(0, 6)
  })

  it('suppresses unknown and unsupported CRS previews', () => {
    expect(crsSuppressionStatus(getPreviewCRSTransform({ source_srid: 0 }))).toBe('unknown_crs')
    expect(crsSuppressionStatus(getPreviewCRSTransform({ source_srid: 4547 }))).toBe('unsupported_crs')
  })
})
