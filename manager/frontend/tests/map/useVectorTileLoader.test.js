import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useVectorTileLoader } from '../../../../common-frontend/map/src/composables/useVectorTileLoader.js'

vi.mock('ol/layer/VectorTile.js', () => ({
  default: class VectorTileLayer {
    constructor(options) {
      this.options = options
    }
    getSource() {
      return this.options.source
    }
    set() {}
  }
}))

vi.mock('ol/source/VectorTile.js', () => ({
  default: class VectorTileSource {
    constructor(options) {
      this.options = options
      this.refresh = vi.fn()
    }
    setTileLoadFunction(fn) {
      this.tileLoadFunction = fn
    }
    getTileGrid() {
      return {
        getTileCoordExtent: () => [0, 0, 1, 1]
      }
    }
    getProjection() {
      return 'EPSG:3857'
    }
  }
}))

vi.mock('ol/format/MVT.js', () => ({
  default: class MVT {
    readFeatures() {
      return []
    }
  }
}))

vi.mock('ol/TileState.js', () => ({
  default: {
    ERROR: 3
  }
}))

vi.mock('../../../../common-frontend/map/src/utils/mapStyles.js', () => ({
  createDefaultStyleFunction: () => vi.fn()
}))

describe('useVectorTileLoader', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('window', {
      setTimeout: globalThis.setTimeout,
      clearTimeout: globalThis.clearTimeout
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('uses Retry-After as degraded tile refresh cooldown', async () => {
    const response = new Response(new ArrayBuffer(0), {
      status: 200,
      headers: {
        'X-ADDP-Tile-Status': 'timeout',
        'X-ADDP-Tile-Retry-Policy': 'ttl',
        'Retry-After': '45'
      }
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response))

    const { createVectorTileLayer, cleanup } = useVectorTileLoader()
    const layer = createVectorTileLayer('/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt', () => '', {
      degradedRetryCooldownMs: 15000
    })
    const source = layer.getSource()
    const tile = {
      getFormat: () => null,
      setFeatures: vi.fn(),
      setState: vi.fn()
    }

    source.tileLoadFunction(tile, '/api/v1/manager/quick-view/tiles/6/1/2.mvt')
    await vi.runAllTicks()
    await vi.advanceTimersByTimeAsync(44999)
    expect(source.refresh).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(source.refresh).toHaveBeenCalledTimes(1)
    cleanup()
  })
})
