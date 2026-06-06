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

  it('does not use legacy srid metadata as the preview CRS contract', () => {
    const preview = {
      srid: 4326,
      object: {
        content: {
          metadata: {
            srid: 4326,
            spatial_ref_sys: 'EPSG:4326'
          }
        }
      }
    }

    expect(sourceSRIDFromPreview(preview)).toBe(0)
    expect(sourceCRSFromPreview(preview)).toBe('')
    expect(crsSuppressionStatus(getPreviewCRSTransform(preview))).toBe('unknown_crs')
  })

  it('does not treat source_crs id as a CRS definition', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 4549,
      source_crs: 'EPSG:4549'
    })

    expect(transform.status).toBe('unsupported_crs')
  })

  it('does not trust target_srid without an engine transformed contract', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 4549,
      source_crs: 'EPSG:4549',
      target_srid: 4326
    })

    expect(transform.status).toBe('unsupported_crs')
  })

  it('accepts target_srid only for engine transformed preview geometry', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 4549,
      source_crs: 'EPSG:4549',
      target_srid: 4326,
      transform_status: 'engine_transformed'
    })

    expect(transform.status).toBe('direct')
  })

  it('registers an explicit CRS definition for preview transform', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 4549,
      source_crs: 'EPSG:4549',
      source_crs_definition: {
        id: 'EPSG:4549',
        definition_encoding: 'proj4',
        definition: '+proj=tmerc +lat_0=0 +lon_0=120 +k=1 +x_0=500000 +y_0=0 +ellps=GRS80 +units=m +no_defs +type=crs',
        source: 'postgis_spatial_ref_sys'
      }
    })
    const geometry = transformGeoJSONGeometryToWGS84({
      type: 'Point',
      coordinates: [500000, 3400000]
    }, transform)

    expect(transform.status).toBe('transformable')
    expect(geometry.coordinates[0]).toBeCloseTo(120, 6)
    expect(geometry.coordinates[1]).toBeCloseTo(30.720617, 6)
  })

  it('registers a sidecar PRJ ESRI WKT definition for preview transform', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 32650,
      source_crs: 'EPSG:32650',
      source_crs_definition: {
        id: 'EPSG:32650',
        definition_encoding: 'esri_wkt',
        definition: [
          'PROJCS["WGS_1984_UTM_Zone_50N"',
          'GEOGCS["GCS_WGS_1984"',
          'DATUM["D_WGS_1984"',
          'SPHEROID["WGS_1984",6378137.0,298.257223563]]',
          'PRIMEM["Greenwich",0.0]',
          'UNIT["Degree",0.0174532925199433]]',
          'PROJECTION["Transverse_Mercator"]',
          'PARAMETER["False_Easting",500000.0]',
          'PARAMETER["False_Northing",0.0]',
          'PARAMETER["Central_Meridian",117.0]',
          'PARAMETER["Scale_Factor",0.9996]',
          'PARAMETER["Latitude_Of_Origin",0.0]',
          'UNIT["Meter",1.0]]'
        ].join(','),
        source: 'sidecar_prj'
      }
    })
    const geometry = transformGeoJSONGeometryToWGS84({
      type: 'Point',
      coordinates: [500000, 0]
    }, transform)

    expect(transform.status).toBe('transformable')
    expect(geometry.coordinates[0]).toBeCloseTo(117, 6)
    expect(geometry.coordinates[1]).toBeCloseTo(0, 6)
  })

  it('registers a PostGIS WKT definition for preview transform', () => {
    const transform = getPreviewCRSTransform({
      source_srid: 32650,
      source_crs: 'EPSG:32650',
      source_crs_definition: {
        id: 'EPSG:32650',
        definition_encoding: 'wkt',
        definition: [
          'PROJCS["WGS 84 / UTM zone 50N"',
          'GEOGCS["WGS 84"',
          'DATUM["WGS_1984"',
          'SPHEROID["WGS 84",6378137,298.257223563,AUTHORITY["EPSG","7030"]]',
          'AUTHORITY["EPSG","6326"]]',
          'PRIMEM["Greenwich",0,AUTHORITY["EPSG","8901"]]',
          'UNIT["degree",0.0174532925199433,AUTHORITY["EPSG","9122"]]',
          'AUTHORITY["EPSG","4326"]]',
          'PROJECTION["Transverse_Mercator"]',
          'PARAMETER["latitude_of_origin",0]',
          'PARAMETER["central_meridian",117]',
          'PARAMETER["scale_factor",0.9996]',
          'PARAMETER["false_easting",500000]',
          'PARAMETER["false_northing",0]',
          'UNIT["metre",1,AUTHORITY["EPSG","9001"]]',
          'AXIS["Easting",EAST]',
          'AXIS["Northing",NORTH]',
          'AUTHORITY["EPSG","32650"]]'
        ].join(','),
        source: 'postgis_spatial_ref_sys'
      }
    })
    const geometry = transformGeoJSONGeometryToWGS84({
      type: 'Point',
      coordinates: [500000, 0]
    }, transform)

    expect(transform.status).toBe('transformable')
    expect(geometry.coordinates[0]).toBeCloseTo(117, 6)
    expect(geometry.coordinates[1]).toBeCloseTo(0, 6)
  })
})
