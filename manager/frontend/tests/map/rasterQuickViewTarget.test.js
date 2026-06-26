import { describe, expect, it } from 'vitest'
import {
  isTIFFRasterMeta,
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
})
