import { describe, expect, it } from 'vitest'
import {
  isVectorTileObjectPreview,
  vectorTileObjectPreviewProps
} from '../../src/utils/vectorTileObjectPreview.js'

describe('vectorTileObjectPreview', () => {
  const data = {
    object: {
      content: {
        frontend_renderer: 'vector_tile',
        preview_material: 'url',
        url: '/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator=item',
        metadata: {
          locator: 'addp://engine/12/path/manager/roads.pmtiles?type=object&item_id=1',
          render_source: 'business_pmtiles',
          min_zoom: 3,
          max_zoom: 12,
          extent: [116.397, 31.23, 121.474, 39.908],
          extent_srid: 4326
        }
      }
    }
  }

  it('selects the vector tile renderer from backend preview semantics', () => {
    expect(isVectorTileObjectPreview(data)).toBe(true)
    expect(isVectorTileObjectPreview({ object: { content: { kind: 'vector_tile' } } })).toBe(false)
  })

  it('maps the controlled locator tile URL and PMTiles facts to the existing renderer', () => {
    expect(vectorTileObjectPreviewProps(data)).toEqual({
      locator: data.object.content.metadata.locator,
      tileUrlTemplate: data.object.content.url,
      tileRenderInfo: data.object.content.metadata,
      renderSource: 'business_pmtiles'
    })
  })
})
