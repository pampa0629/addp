import Feature from 'ol/Feature.js'
import LineString from 'ol/geom/LineString.js'
import Point from 'ol/geom/Point.js'
import VectorLayer from 'ol/layer/Vector.js'
import VectorSource from 'ol/source/Vector.js'
import { Fill, Stroke, Style, Text } from 'ol/style.js'

const webMercatorHalfWorld = 20037508.342789244
const webMercatorWorldSize = webMercatorHalfWorld * 2
const maxMvtGridTiles = 256
const maxMvtGridStatusEntries = 512

const mvtGridKindColor = {
  unknown: 'rgba(255, 45, 32, 0.95)',
  cache: 'rgba(35, 128, 255, 0.95)',
  dynamic: 'rgba(245, 177, 50, 0.95)',
  ok: 'rgba(54, 179, 126, 0.95)',
  empty: 'rgba(148, 163, 184, 0.9)',
  degraded: 'rgba(255, 145, 0, 0.98)',
  timeout: 'rgba(255, 145, 0, 0.98)',
  error: 'rgba(245, 63, 63, 0.98)'
}

const mvtGridKindWidth = {
  unknown: 2,
  cache: 2,
  dynamic: 2,
  ok: 2,
  empty: 1.5,
  degraded: 3,
  timeout: 3,
  error: 3
}

export function useMvtGridDebug({ t, isVisible }) {
  const tileStates = new globalThis.Map()
  const lineStyleCache = new globalThis.Map()
  const labelStyleCache = new globalThis.Map()
  let gridLayer = null

  function mvtGridStyle(feature) {
    const debugKind = feature.get('tileDebugKind') || 'unknown'
    const color = mvtGridKindColor[debugKind] || mvtGridKindColor.unknown
    if (feature.get('kind') !== 'label') {
      const lineKey = `${debugKind}:${color}`
      if (!lineStyleCache.has(lineKey)) {
        lineStyleCache.set(lineKey, new Style({
          stroke: new Stroke({
            color,
            width: mvtGridKindWidth[debugKind] || 2,
            lineDash: ['timeout', 'degraded', 'empty'].includes(debugKind) ? [8, 5] : undefined
          })
        }))
      }
      return lineStyleCache.get(lineKey)
    }
    const label = feature.get('label') || ''
    const labelKey = `${debugKind}:${label}`
    if (!labelStyleCache.has(labelKey)) {
      labelStyleCache.set(labelKey, new Style({
        text: new Text({
          text: label,
          font: '700 13px Arial, sans-serif',
          fill: new Fill({ color }),
          stroke: new Stroke({ color: 'rgba(255, 255, 255, 0.96)', width: 5 }),
          padding: [2, 4, 2, 4],
          overflow: true
        })
      }))
    }
    return labelStyleCache.get(labelKey)
  }

  function tileKey(z, x, y) {
    return `${z}/${x}/${y}`
  }

  function statusLabel(kind) {
    const key = `manager.spatialPreview.mvtGridStatus.${kind || 'unknown'}`
    const label = t(key)
    return label === key ? (kind || '') : label
  }

  function kindFromMeta(meta = {}, hasError = false) {
    if (meta.oversized || (hasError && !meta.cooledDown && !meta.suppressed)) return 'error'
    const semanticStatus = String(meta.tileStatus || '').toLowerCase()
    if (semanticStatus === 'timeout') return 'timeout'
    if (semanticStatus === 'degraded' || meta.cooledDown || meta.suppressed) return 'degraded'
    if (semanticStatus === 'empty') return 'empty'
    const cacheStatus = String(meta.cacheStatus || '').toUpperCase()
    if (cacheStatus === 'HIT') return 'cache'
    if (cacheStatus === 'MISS') return 'dynamic'
    if (semanticStatus === 'ok') return 'ok'
    return 'unknown'
  }

  function rememberTileState(meta = {}, hasError = false, map = null) {
    if (!isVisible() || !meta.tileKey) return
    const kind = kindFromMeta(meta, hasError)
    tileStates.set(meta.tileKey, {
      kind,
      status: String(meta.tileStatus || '').toLowerCase(),
      cacheStatus: String(meta.cacheStatus || '').toUpperCase(),
      featureCount: Number.isFinite(Number(meta.featureCount)) ? Number(meta.featureCount) : null,
      oversized: !!meta.oversized,
      cooledDown: !!meta.cooledDown,
      suppressed: !!meta.suppressed,
      updatedAt: Date.now()
    })
    while (tileStates.size > maxMvtGridStatusEntries) {
      const oldestKey = tileStates.keys().next().value
      tileStates.delete(oldestKey)
    }
    updateGrid(map)
  }

  function labelForTile(key, state) {
    if (!state || state.kind === 'unknown') return key
    return `${key}\n${statusLabel(state.kind)}`
  }

  function pruneTileStates(range, z) {
    for (const key of tileStates.keys()) {
      const [stateZ, stateX, stateY] = key.split('/').map(Number)
      if (
        stateZ !== z ||
        stateX < range.minX ||
        stateX > range.maxX ||
        stateY < range.minY ||
        stateY > range.maxY
      ) {
        tileStates.delete(key)
      }
    }
  }

  function tileRangeForExtent(extent, z) {
    const tilesPerAxis = 2 ** z
    const tileSize = webMercatorWorldSize / tilesPerAxis
    const clamp = (value) => Math.max(0, Math.min(tilesPerAxis - 1, value))
    return {
      minX: clamp(Math.floor((extent[0] + webMercatorHalfWorld) / tileSize)),
      maxX: clamp(Math.floor((extent[2] + webMercatorHalfWorld) / tileSize)),
      minY: clamp(Math.floor((webMercatorHalfWorld - extent[3]) / tileSize)),
      maxY: clamp(Math.floor((webMercatorHalfWorld - extent[1]) / tileSize)),
      tileSize
    }
  }

  function tileExtent(z, x, y) {
    const tilesPerAxis = 2 ** z
    const tileSize = webMercatorWorldSize / tilesPerAxis
    const minX = -webMercatorHalfWorld + x * tileSize
    const maxY = webMercatorHalfWorld - y * tileSize
    return [minX, maxY - tileSize, minX + tileSize, maxY]
  }

  function createGridLayer() {
    gridLayer = new VectorLayer({
      source: new VectorSource(),
      style: mvtGridStyle,
      visible: isVisible(),
      zIndex: 30,
      declutter: true
    })
    return gridLayer
  }

  function updateGrid(map) {
    if (!map || !gridLayer) return
    gridLayer.setVisible(isVisible())
    const source = gridLayer.getSource()
    if (!source) return
    source.clear()
    if (!isVisible()) return

    const view = map.getView()
    const size = map.getSize()
    if (!view || !size) return
    const z = Math.max(0, Math.round(view.getZoom() || 0))
    const range = tileRangeForExtent(view.calculateExtent(size), z)
    const tileCount = (range.maxX - range.minX + 1) * (range.maxY - range.minY + 1)
    if (tileCount > maxMvtGridTiles) return

    const features = []
    for (let x = range.minX; x <= range.maxX; x += 1) {
      for (let y = range.minY; y <= range.maxY; y += 1) {
        const ext = tileExtent(z, x, y)
        const key = tileKey(z, x, y)
        const tileState = tileStates.get(key)
        const tileDebugKind = tileState?.kind || 'unknown'
        features.push(new Feature({
          geometry: new LineString([
            [ext[0], ext[1]],
            [ext[2], ext[1]],
            [ext[2], ext[3]],
            [ext[0], ext[3]],
            [ext[0], ext[1]]
          ]),
          kind: 'grid',
          tileDebugKind
        }))
        features.push(new Feature({
          geometry: new Point([(ext[0] + ext[2]) / 2, (ext[1] + ext[3]) / 2]),
          kind: 'label',
          tileDebugKind,
          label: labelForTile(key, tileState)
        }))
      }
    }
    pruneTileStates(range, z)
    source.addFeatures(features)
  }

  function resetGrid(map) {
    tileStates.clear()
    updateGrid(map)
  }

  function clearTileStates() {
    tileStates.clear()
  }

  function disposeGrid() {
    gridLayer = null
    tileStates.clear()
    lineStyleCache.clear()
    labelStyleCache.clear()
  }

  return {
    createGridLayer,
    updateGrid,
    resetGrid,
    clearTileStates,
    rememberTileState,
    disposeGrid
  }
}
