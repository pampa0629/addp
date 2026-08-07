/**
 * 地图底图配置
 */
import TileLayer from 'ol/layer/Tile.js'
import XYZ from 'ol/source/XYZ.js'

/**
 * 创建高德地图底图图层
 * @returns {TileLayer} OpenLayers TileLayer
 */
export function createGaodeBaseLayer(options = {}) {
  return new TileLayer({
    source: new XYZ({
      urls: [
        'https://webrd01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd02.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd03.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd04.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}'
      ],
      crossOrigin: 'anonymous',
      maxZoom: options.maxZoom || 18,
      wrapX: true
    }),
    opacity: options.opacity ?? 1,
    zIndex: options.zIndex ?? 1
  })
}

/**
 * 创建天地图 WebMercator 底图和注记图层
 * @returns {TileLayer[]}
 */
export function createTiandituBaseLayers(options = {}) {
  const key = String(options.key || '').trim()
  if (!key) return []

  const type = options.type === 'image' ? 'image' : 'vector'
  const isImage = type === 'image'
  const baseId = isImage ? 'img' : 'vec'
  const labelId = isImage ? 'cia' : 'cva'
  const maxZoom = options.maxZoom || 18

  const createLayer = (layerId, zIndex) => new TileLayer({
    source: new XYZ({
      url: `https://t{0-7}.tianditu.gov.cn/${layerId}_w/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=${layerId}&STYLE=default&TILEMATRIXSET=w&FORMAT=tiles&TILEMATRIX={z}&TILEROW={y}&TILECOL={x}&tk=${key}`,
      crossOrigin: 'anonymous',
      maxZoom,
      wrapX: true
    }),
    opacity: options.opacity ?? 1,
    zIndex
  })

  return [
    createLayer(baseId, options.zIndex ?? 0),
    createLayer(labelId, options.labelZIndex ?? 100)
  ]
}

/**
 * 创建 OpenStreetMap 底图图层
 * @returns {TileLayer} OpenLayers TileLayer
 */
export function createOSMBaseLayer(options = {}) {
  return new TileLayer({
    source: new XYZ({
      url: 'https://{a-c}.tile.openstreetmap.org/{z}/{x}/{y}.png',
      attributions: '© OpenStreetMap contributors',
      crossOrigin: 'anonymous',
      maxZoom: options.maxZoom || 19,
      wrapX: true
    }),
    opacity: options.opacity ?? 1,
    zIndex: options.zIndex ?? 0
  })
}
