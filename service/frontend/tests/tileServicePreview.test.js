import test from 'node:test'
import assert from 'node:assert/strict'
import { tilePreviewConfig, tilePreviewCoordinate } from '../src/utils/tileServicePreview.js'

test('uses PMTiles snapshot camera and zoom range', () => {
  const layer = {
    layer_config: {
      source_snapshot: {
        center: [111.4499249, 27.3849525, 4],
        min_zoom: 4,
        max_zoom: 12,
        spatial: {
          srid: 4326,
          extent: [108.5564817, 24.5258548, 114.343368, 30.2440502]
        }
      }
    }
  }

  assert.deepEqual(tilePreviewConfig(layer), {
    center: [111.4499249, 27.3849525],
    extent: [108.5564817, 24.5258548, 114.343368, 30.2440502],
    zoom: 4,
    minZoom: 4,
    maxZoom: 12
  })
  assert.deepEqual(tilePreviewCoordinate(layer), { z: 4, x: 12, y: 6 })
})

test('derives a neutral camera from spatial extent without a hardcoded region', () => {
  const layer = {
    layer_config: {
      source_snapshot: {
        min_zoom: 3,
        max_zoom: 8,
        spatial: { extent: [10, 20, 14, 28] }
      }
    }
  }

  assert.deepEqual(tilePreviewConfig(layer), {
    center: [12, 24],
    extent: [10, 20, 14, 28],
    zoom: 3,
    minZoom: 3,
    maxZoom: 8
  })
})

test('falls back to the published center when the spatial extent is invalid', () => {
  const layer = {
    layer_config: {
      source_snapshot: {
        center: [111, 27, 6],
        min_zoom: 4,
        max_zoom: 12,
        spatial: { extent: [114, 30, 108, 24] }
      }
    }
  }

  assert.deepEqual(tilePreviewConfig(layer), {
    center: [111, 27],
    extent: null,
    zoom: 6,
    minZoom: 4,
    maxZoom: 12
  })
})
