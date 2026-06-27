import { describe, expect, it } from 'vitest'
import {
  isRasterMosaicMeta,
  isTIFFRasterMeta,
  rasterExtentLooksGeographic,
  rasterExtentSRIDFromMetadata,
  rasterMetaAttributes,
  rasterSpatialFacts
} from '../../src/utils/rasterQuickViewTarget'

describe('rasterQuickViewTarget', () => {
  it('identifies multi TIFF item from meta attributes without relying on node suffix', () => {
    const previewData = {
      object: {
        attributes: {
          item: {
            data_type: 'media',
            format: 'tiff',
            layout: 'multi'
          },
          format_info: {
            tiff: {
              profile: 'geotiff'
            }
          },
          capabilities: {
            spatial: {
              srid: 4326,
              extent: [100, 20, 101, 21]
            }
          }
        }
      }
    }
    const selectedNode = {
      label: 'srtm_40_01',
      path: 'addp/image/srtm_40_01'
    }

    expect(isTIFFRasterMeta(previewData, selectedNode)).toBe(true)
    expect(rasterSpatialFacts(previewData, selectedNode)).toEqual({
      srid: 4326,
      extent: [100, 20, 101, 21]
    })
  })

  it('uses selected node attributes when preview object attributes are not present', () => {
    const selectedNode = {
      attributes: {
        item: {
          data_type: 'media',
          format: 'tiff'
        }
      }
    }

    expect(isTIFFRasterMeta({}, selectedNode)).toBe(true)
    expect(rasterMetaAttributes({}, selectedNode)).toBe(selectedNode.attributes)
  })

  it('does not treat arbitrary media nodes as TIFF raster', () => {
    const previewData = {
      object: {
        attributes: {
          item: {
            data_type: 'media',
            format: 'png'
          }
        }
      }
    }

    expect(isTIFFRasterMeta(previewData, {})).toBe(false)
  })

  it('identifies whole raster mosaic item from meta attributes', () => {
    const previewData = {
      object: {
        attributes: {
          item: {
            data_type: 'media',
            format: 'raster_mosaic',
            layout: 'whole'
          },
          format_info: {
            raster_mosaic: {
              overview_ref: 'overviews/overview.cog.tif',
              leaf_count: 2360
            }
          },
          capabilities: {
            spatial: {
              srid: 4326,
              extent_srid: 4326,
              extent: [100, 20, 101, 21]
            }
          }
        }
      }
    }

    expect(isRasterMosaicMeta(previewData, {})).toBe(true)
    expect(rasterSpatialFacts(previewData, {})).toEqual({
      srid: 4326,
      extent_srid: 4326,
      extent: [100, 20, 101, 21]
    })
  })

  it('requires whole layout for raster mosaic quick view target', () => {
    const previewData = {
      object: {
        attributes: {
          item: {
            data_type: 'media',
            format: 'raster_mosaic',
            layout: 'component'
          }
        }
      }
    }

    expect(isRasterMosaicMeta(previewData, {})).toBe(false)
  })

  it('infers EPSG:4326 for raster leaf metadata with geographic extent', () => {
    const extent = [110.994845715, 2.994830145, 112.50516055500002, 4.004964585]

    expect(rasterExtentLooksGeographic(extent)).toBe(true)
    expect(rasterExtentSRIDFromMetadata({ extent }, extent)).toBe(4326)
    expect(rasterExtentSRIDFromMetadata({ extent_srid: 3857 }, extent)).toBe(3857)
    expect(rasterExtentSRIDFromMetadata({ extent: [500000, 3000000, 510000, 3010000] })).toBe(0)
  })
})
