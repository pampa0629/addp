/**
 * 地图底图配置
 */
import TileLayer from 'ol/layer/Tile.js'
import XYZ from 'ol/source/XYZ.js'

/**
 * 创建高德地图底图图层
 * @returns {TileLayer} OpenLayers TileLayer
 */
export function createGaodeBaseLayer() {
  return new TileLayer({
    source: new XYZ({
      urls: [
        'https://webrd01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd02.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd03.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd04.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}'
      ],
      crossOrigin: 'anonymous'
    }),
    zIndex: 0  // 底图在最下层
  })
}

/**
 * 创建 OpenStreetMap 底图图层
 * @returns {TileLayer} OpenLayers TileLayer
 */
export function createOSMBaseLayer() {
  return new TileLayer({
    source: new XYZ({
      url: 'https://{a-c}.tile.openstreetmap.org/{z}/{x}/{y}.png',
      crossOrigin: 'anonymous'
    }),
    zIndex: 0
  })
}
