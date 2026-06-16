/**
 * Vector Tile 瓦片加载逻辑的组合式函数
 */
import VectorTileLayer from 'ol/layer/VectorTile.js'
import VectorTileSource from 'ol/source/VectorTile.js'
import MVT from 'ol/format/MVT.js'
import TileState from 'ol/TileState.js'
import { createDefaultStyleFunction } from '../utils/mapStyles.js'

/**
 * 创建和管理Vector Tile图层
 * @param {Object} options - 配置选项
 * @returns {Object} Vector Tile相关的方法
 */
export function useVectorTileLoader(options = {}) {
  const { token } = options
  const defaultMaxDecodedTileBytes = 8 * 1024 * 1024
  const defaultDegradedRetryCooldownMs = 15000

  // 跟踪所有进行中的瓦片请求
  const activeTileRequests = new Map()
  const degradedTileCooldowns = new Map()
  const suppressedTileKeys = new Set()
  const degradedSourceRefreshTimers = new Map()
  let requestSeq = 0

  /**
   * 构建瓦片唯一标识
   * @param {number} z - 缩放级别
   * @param {number} x - X坐标
   * @param {number} y - Y坐标
   * @returns {string} 瓦片key
   */
  function buildTileKey(z, x, y) {
    return `${z}/${x}/${y}`
  }

  function buildRequestKey(tileKey) {
    requestSeq += 1
    return `${tileKey}#${requestSeq}`
  }

  function degradedCooldownKey(src) {
    return src
  }

  function isDegradedTileStatus(status) {
    return ['timeout', 'degraded'].includes(String(status || '').toLowerCase())
  }

  function tileResponseMeta(res, tileKey) {
    return {
      tileKey,
      renderSource: res.headers.get('X-ADDP-Render-Source') || '',
      tileCacheId: res.headers.get('X-ADDP-Tile-Cache-ID') || '',
      cacheStatus: res.headers.get('X-ADDP-Tile-Cache') || '',
      tileStatus: res.headers.get('X-ADDP-Tile-Status') || '',
      performanceMode: res.headers.get('X-ADDP-Tile-Performance-Mode') || '',
      recommendation: res.headers.get('X-ADDP-Tile-Recommendation') || '',
      retryPolicy: res.headers.get('X-ADDP-Tile-Retry-Policy') || '',
      retryAfter: res.headers.get('Retry-After') || '',
      generationTime: res.headers.get('X-Generation-Time') || '',
      contentLength: res.headers.get('Content-Length') || ''
    }
  }

  function isTileInDegradedCooldown(key) {
    const expiresAt = degradedTileCooldowns.get(key)
    if (!expiresAt) return false
    if (Date.now() < expiresAt) return true
    degradedTileCooldowns.delete(key)
    return false
  }

  function scheduleDegradedTileRefresh(source, key, options = {}) {
    const cooldownMs = options.degradedRetryCooldownMs || defaultDegradedRetryCooldownMs
    const expiresAt = Date.now() + cooldownMs
    degradedTileCooldowns.set(key, expiresAt)
    if (degradedSourceRefreshTimers.has(source)) return
    const timer = window.setTimeout(() => {
      degradedSourceRefreshTimers.delete(source)
      const now = Date.now()
      degradedTileCooldowns.forEach((tileExpiresAt, tileKey) => {
        if (tileExpiresAt <= now) {
          degradedTileCooldowns.delete(tileKey)
        }
      })
      source?.refresh?.()
    }, cooldownMs)
    degradedSourceRefreshTimers.set(source, timer)
  }

  function shouldSuppressTile(meta = {}) {
    return isDegradedTileStatus(meta.tileStatus) &&
      String(meta.retryPolicy || '').toLowerCase() === 'suppress_tile'
  }

  function handleDecodedTileBuffer(tile, buf, meta, tileKey, extent, projection, options = {}, source = null, src = '') {
    if (src && shouldSuppressTile(meta)) {
      suppressedTileKeys.add(degradedCooldownKey(src))
    } else if (source && src && isDegradedTileStatus(meta.tileStatus)) {
      scheduleDegradedTileRefresh(source, degradedCooldownKey(src), options)
    }

    const maxDecodedTileBytes = options.maxDecodedTileBytes || defaultMaxDecodedTileBytes
    if (buf.byteLength > maxDecodedTileBytes) {
      const error = new Error(`MVT tile too large: ${Math.ceil(buf.byteLength / 1024 / 1024)}MB`)
      options.onTileLoadError?.({
        ...meta,
        tileStatus: meta.tileStatus || 'degraded',
        tileKey,
        error,
        oversized: true,
        decodedBytes: buf.byteLength
      })
      if (source && src) {
        scheduleDegradedTileRefresh(source, degradedCooldownKey(src), options)
      }
      tile.setFeatures([])
      return
    }

    if (buf.byteLength === 0) {
      options.onTileLoadEnd?.({
        ...meta,
        featureCount: 0
      })
      tile.setFeatures([])
      return
    }

    const format = tile.getFormat() || new MVT()
    const features = format.readFeatures(buf, {
      extent: extent,
      featureProjection: projection
    })
    if (options.debugTileLoading) {
      console.debug(`切片 ${tileKey} 加载成功，包含 ${features.length} 个要素`, features.length > 0 ? `第一个要素类型: ${features[0].getGeometry()?.getType()}` : '')
    }
    options.onTileLoadEnd?.({
      ...meta,
      featureCount: features.length
    })
    tile.setFeatures(features)
  }

  /**
   * 创建瓦片加载函数
   * @param {Function} getToken - 获取token的函数
   * @param {VectorTileSource} source - VectorTileSource实例，用于获取tileGrid
   * @returns {Function} 瓦片加载函数
   */
  function createTileLoadFunction(getToken, source, options = {}) {
    return (tile, src) => {
      // 提取瓦片坐标
      const match = src.match(/\/tiles\/(?:[^/]+\/[^/]+\/)?(\d+)\/(\d+)\/(\d+)(?:\.mvt)?(?:\?|$)/)
      if (!match) {
        console.warn('无法解析瓦片URL:', src)
        tile.setState(TileState.ERROR)
        return
      }

      const z = parseInt(match[1], 10)
      const x = parseInt(match[2], 10)
      const y = parseInt(match[3], 10)
      const tileKey = buildTileKey(z, x, y)

      // 获取瓦片的extent和projection
      const tileCoord = [z, x, y]
      const tileGrid = source.getTileGrid()
      const extent = tileGrid.getTileCoordExtent(tileCoord)
      const projection = source.getProjection()
      const cooldownKey = degradedCooldownKey(src)

      if (isTileInDegradedCooldown(cooldownKey)) {
        options.onTileLoadError?.({
          tileKey,
          tileStatus: 'degraded',
          cooledDown: true,
          error: new Error('MVT tile is in degraded retry cooldown')
        })
        tile.setFeatures([])
        return
      }
      if (suppressedTileKeys.has(cooldownKey)) {
        options.onTileLoadError?.({
          tileKey,
          tileStatus: 'timeout',
          retryPolicy: 'suppress_tile',
          suppressed: true,
          error: new Error('MVT tile is suppressed after source-transform timeout')
        })
        tile.setFeatures([])
        return
      }

      // 创建新的 AbortController
      const controller = new AbortController()
      const requestKey = buildRequestKey(tileKey)
      activeTileRequests.set(requestKey, { controller, tileKey })

      // 发起请求 (关联取消信号)
      fetch(src, {
        headers: { Authorization: getToken() ? `Bearer ${getToken()}` : '' },
        signal: controller.signal
      })
        .then(res => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
          const meta = tileResponseMeta(res, tileKey)
          return res.arrayBuffer().then(buf => ({ buf, meta }))
        })
        .then(({ buf, meta }) => {
          handleDecodedTileBuffer(tile, buf, meta, tileKey, extent, projection, options, source, src)
        })
        .catch(e => {
          if (e.name === 'AbortError') {
            if (options.debugTileLoading) {
              console.debug('瓦片请求已取消:', tileKey)
            }
            return
          }
          console.error('加载切片失败:', src, e)
          options.onTileLoadError?.({ tileKey, error: e })
          tile.setState(TileState.ERROR)
        })
        .finally(() => {
          activeTileRequests.delete(requestKey)
        })
    }
  }

  /**
   * 创建 Vector Tile 图层
   * @param {string} urlTemplate - 瓦片URL模板
   * @param {Function} getToken - 获取token的函数
   * @param {Object} options - 可选配置
   * @param {number} options.minZoom - 最小瓦片层级（数据层级，不是地图层级）
   * @param {number} options.maxZoom - 最大瓦片层级（数据层级，不是地图层级）
   * @returns {VectorTileLayer} OpenLayers VectorTileLayer
   */
  function createVectorTileLayer(urlTemplate, getToken, options = {}) {
    const vtSource = new VectorTileSource({
      format: new MVT(),
      cacheSize: options.cacheSize || 64,
      minZoom: options.minZoom || 0,  // 瓦片数据的最小层级
      maxZoom: options.maxZoom || 20,  // 瓦片数据的最大层级
      url: urlTemplate  // 使用 URL 模板（OpenLayers 会自动替换 {z}/{x}/{y}）
    })

    // 在创建source后设置tileLoadFunction，这样可以访问source对象
    vtSource.setTileLoadFunction(createTileLoadFunction(getToken, vtSource, options))

    return new VectorTileLayer({
      source: vtSource,
      style: createDefaultStyleFunction(),
      zIndex: 10
    })
  }

  /**
   * 清理所有请求
   */
  function cleanup() {
    if (activeTileRequests && activeTileRequests.forEach) {
      activeTileRequests.forEach(({ controller }) => {
        controller?.abort()
      })
      activeTileRequests.clear()
    }
    degradedSourceRefreshTimers.forEach((timer) => window.clearTimeout(timer))
    degradedSourceRefreshTimers.clear()
    degradedTileCooldowns.clear()
    suppressedTileKeys.clear()
  }

  return {
    activeTileRequests,
    createVectorTileLayer,
    cleanup
  }
}
