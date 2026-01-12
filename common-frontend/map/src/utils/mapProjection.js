/**
 * 地图投影和坐标转换工具函数
 */

/**
 * 将经纬度坐标转换为 Web Mercator (EPSG:3857) 投影坐标
 * @param {Array} lonLat - [经度, 纬度]
 * @returns {Array} [x, y] Web Mercator 坐标
 */
export function fromLonLat(lonLat) {
  const [lon, lat] = lonLat
  const x = (lon * 20037508.34) / 180
  let y = Math.log(Math.tan(((90 + lat) * Math.PI) / 360)) / (Math.PI / 180)
  y = (y * 20037508.34) / 180
  return [x, y]
}

/**
 * 将 Web Mercator (EPSG:3857) 投影坐标转换为经纬度
 * @param {Array} coord - [x, y] Web Mercator 坐标
 * @returns {Array} [经度, 纬度]
 */
export function toLonLat(coord) {
  const [x, y] = coord
  const lon = (x / 20037508.34) * 180
  let lat = (y / 20037508.34) * 180
  lat = 180 / Math.PI * (2 * Math.atan(Math.exp(lat * Math.PI / 180)) - Math.PI / 2)
  return [lon, lat]
}
