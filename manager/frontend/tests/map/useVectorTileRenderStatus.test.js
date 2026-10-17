import { describe, expect, it } from 'vitest'
import { useVectorTileRenderStatus } from '../../src/composables/useVectorTileRenderStatus.js'

function setup() {
  let keys = ['5/26/13']
  const status = useVectorTileRenderStatus({
    t: (key, args) => args?.count === undefined ? key : `${key}:${args.count}`,
    getRenderSource: () => 'business_pmtiles', getDefaultTileCacheId: () => '',
    getVisibleTileKeys: () => keys
  })
  let callbacks = status.beginTileSource()
  const load = (tileKey, featureCount = 2) => callbacks.onTileLoadEnd({ tileKey, featureCount, cacheStatus: 'MISS', tileStatus: 'ok', renderSource: 'business_pmtiles' })
  return { status, load, callbacks, begin: () => { callbacks = status.beginTileSource(); return callbacks }, view: value => { keys = value; status.refreshTileStatus() } }
}

describe('visible vector tile render status', () => {
  it('preserves decoded observations across a cached viewport render and counts each tile once', () => {
    const { status, load, view } = setup()
    load('5/26/13'); load('5/26/13')
    expect(status.tileStatus.value.totalFeatureCount).toBe(2)
    view(['5/26/13'])
    expect(status.renderStatusClass.value).toBe('is-dynamic')
    expect(status.tileStatus.value.totalFeatureCount).toBe(2)
    expect(status.tileStatus.value.loadedTileCount).toBe(1)
  })
  it('aggregates only the current native tile zoom and restores cached observations on return', () => {
    const { status, load, view } = setup()
    load('5/26/13'); load('4/13/6', 9)
    expect(status.tileStatus.value.totalFeatureCount).toBe(2)
    view(['4/13/6'])
    expect(status.tileStatus.value.totalFeatureCount).toBe(9)
    view(['5/26/13'])
    expect(status.tileStatus.value.totalFeatureCount).toBe(2)
    view(['5/27/13'])
    expect(status.renderStatusClass.value).toBe('is-unknown')
  })
  it('rejects late successes and errors from a replaced source', () => {
    const { status, callbacks: old, begin } = setup()
    const current = begin()
    current.onTileLoadEnd({ tileKey:'5/26/13', featureCount:50, cacheStatus:'MISS' })
    old.onTileLoadEnd({ tileKey:'5/26/13', featureCount:2, cacheStatus:'HIT' })
    old.onTileLoadError({ tileKey:'5/26/13', error:new Error('old failure') })
    expect(status.tileStatus.value.totalFeatureCount).toBe(50)
    expect(status.renderStatusClass.value).toBe('is-dynamic')
  })
  it('retains visible errors until the exact tile succeeds and clears observations on source change', () => {
    const { status, load, view, callbacks } = setup()
    callbacks.onTileLoadError({ tileKey:'5/26/13', error:new Error('tile failed') })
    expect(status.renderStatusClass.value).toBe('is-error')
    load('5/27/13')
    expect(status.renderStatusClass.value).toBe('is-error')
    load('5/26/13')
    expect(status.renderStatusClass.value).toBe('is-dynamic')
    status.resetTileStatus(); view(['5/26/13'])
    expect(status.renderStatusClass.value).toBe('is-unknown')
    expect(status.tileStatus.value.totalFeatureCount).toBe(0)
  })
})
