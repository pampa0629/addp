/**
 * Vector Tile 瓦片加载逻辑的组合式函数
 */
import VectorTileLayer from 'ol/layer/VectorTile.js'
import VectorTileSource from 'ol/source/VectorTile.js'
import MVT from 'ol/format/MVT.js'
import { createDefaultStyleFunction } from '../utils/mapStyles.js'

/**
 * 创建和管理Vector Tile图层
 * @param {Object} options - 配置选项
 * @returns {Object} Vector Tile相关的方法
 */
export function useVectorTileLoader(options = {}) {
  const { token } = options

  // 跟踪所有进行中的瓦片请求
  const activeTileRequests = new Map()

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
        tile.setState(3)
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

      // 防御性检查：确保 activeTileRequests 是 Map
      if (!activeTileRequests || typeof activeTileRequests.has !== 'function') {
        console.error('activeTileRequests is not a Map!', activeTileRequests)
        // 降级处理：不使用请求取消，直接发起请求
        fetch(src, {
          headers: { Authorization: getToken() ? `Bearer ${getToken()}` : '' }
        })
          .then(res => {
            if (!res.ok) throw new Error(`HTTP ${res.status}`)
            const meta = {
              tileKey,
              renderSource: res.headers.get('X-ADDP-Render-Source') || '',
              tileCacheId: res.headers.get('X-ADDP-Tile-Cache-ID') || '',
              cacheStatus: res.headers.get('X-Cache-Status') || '',
              generationTime: res.headers.get('X-Generation-Time') || '',
              contentLength: res.headers.get('Content-Length') || ''
            }
            return res.arrayBuffer().then(buf => ({ buf, meta }))
          })
          .then(({ buf, meta }) => {
            const format = tile.getFormat() || new MVT()
            const features = format.readFeatures(buf, {
              extent: extent,
              featureProjection: projection
            })
            console.log(`切片 ${tileKey} 加载成功(降级), 包含 ${features.length} 个要素`, features.length > 0 ? `第一个要素类型: ${features[0].getGeometry()?.getType()}` : '')
            options.onTileLoadEnd?.({
              ...meta,
              featureCount: features.length
            })
            tile.setFeatures(features)
            tile.setState(2)
          })
          .catch(e => {
            console.error('加载切片失败:', src, e)
            options.onTileLoadError?.({ tileKey, error: e })
            tile.setState(3)
          })
        return
      }

      // 取消该瓦片之前未完成的请求
      if (activeTileRequests.has(tileKey)) {
        const oldController = activeTileRequests.get(tileKey)
        oldController.abort()
        console.debug('取消旧瓦片请求:', tileKey)
      }

      // 创建新的 AbortController
      const controller = new AbortController()
      activeTileRequests.set(tileKey, controller)

      // 发起请求 (关联取消信号)
      fetch(src, {
        headers: { Authorization: getToken() ? `Bearer ${getToken()}` : '' },
        signal: controller.signal
      })
        .then(res => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
          const meta = {
            tileKey,
            renderSource: res.headers.get('X-ADDP-Render-Source') || '',
            tileCacheId: res.headers.get('X-ADDP-Tile-Cache-ID') || '',
            cacheStatus: res.headers.get('X-Cache-Status') || '',
            generationTime: res.headers.get('X-Generation-Time') || '',
            contentLength: res.headers.get('Content-Length') || ''
          }
          return res.arrayBuffer().then(buf => ({ buf, meta }))
        })
        .then(({ buf, meta }) => {
          const format = tile.getFormat() || new MVT()
          const features = format.readFeatures(buf, {
            extent: extent,
            featureProjection: projection
          })
          console.log(`切片 ${tileKey} 加载成功，包含 ${features.length} 个要素`, features.length > 0 ? `第一个要素类型: ${features[0].getGeometry()?.getType()}` : '')
          options.onTileLoadEnd?.({
            ...meta,
            featureCount: features.length
          })
          tile.setFeatures(features)
          tile.setState(2) // 设置为已加载状态
        })
        .catch(e => {
          if (e.name === 'AbortError') {
            console.debug('瓦片请求已取消:', tileKey)
            return
          }
          console.error('加载切片失败:', src, e)
          options.onTileLoadError?.({ tileKey, error: e })
          tile.setState(3) // 设置为错误状态
        })
        .finally(() => {
          activeTileRequests.delete(tileKey)
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
      cacheSize: 512,  // 增加客户端缓存,减少重复请求
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
   * 取消不同缩放级别的瓦片请求
   * @param {number} currentZoom - 当前缩放级别
   */
  function cancelDifferentZoomRequests(currentZoom) {
    if (activeTileRequests && activeTileRequests.forEach) {
      activeTileRequests.forEach((controller, tileKey) => {
        const [z] = tileKey.split('/').map(Number)
        if (z !== currentZoom) {
          controller.abort()
          activeTileRequests.delete(tileKey)
          console.debug('取消不同层级的瓦片请求:', tileKey)
        }
      })
    }
  }

  /**
   * 清理所有请求
   */
  function cleanup() {
    if (activeTileRequests && activeTileRequests.forEach) {
      activeTileRequests.forEach((controller) => {
        controller.abort()
      })
      activeTileRequests.clear()
    }
  }

  return {
    activeTileRequests,
    createVectorTileLayer,
    cancelDifferentZoomRequests,
    cleanup
  }
}
