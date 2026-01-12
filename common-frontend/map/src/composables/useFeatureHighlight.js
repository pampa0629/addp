/**
 * 要素高亮功能的组合式函数
 */
import VectorLayer from 'ol/layer/Vector.js'
import VectorSource from 'ol/source/Vector.js'
import GeoJSON from 'ol/format/GeoJSON.js'
import { createHighlightStyle } from '../utils/mapStyles.js'
import { fromLonLat } from '../utils/mapProjection.js'

/**
 * 创建和管理要素高亮图层
 * @returns {Object} 高亮相关的图层和方法
 */
export function useFeatureHighlight() {
  let highlightSource = null
  let highlightLayer = null

  /**
   * 创建高亮图层
   * @returns {VectorLayer} OpenLayers VectorLayer
   */
  function createHighlightLayer() {
    highlightSource = new VectorSource()
    highlightLayer = new VectorLayer({
      source: highlightSource,
      style: createHighlightStyle(),
      zIndex: 20  // 高亮图层在最上层
    })

    return highlightLayer
  }

  /**
   * 根据ID高亮要素
   * @param {Map} map - OpenLayers Map实例
   * @param {string|number} featureId - 要素ID
   * @param {string} geojsonString - GeoJSON字符串
   * @param {Object} centroid - 中心点 {lon, lat}
   * @param {Array} extent - 边界框 [minLon, minLat, maxLon, maxLat]
   */
  function focusFeatureById(map, featureId, geojsonString, centroid, extent) {
    if (!map) return

    // 清除之前的高亮
    if (highlightSource) {
      highlightSource.clear()
    }

    // 如果有GeoJSON几何，显示高亮
    if (geojsonString && highlightSource) {
      try {
        const format = new GeoJSON()
        const feature = format.readFeature(geojsonString, {
          dataProjection: 'EPSG:4326',
          featureProjection: 'EPSG:3857'
        })
        highlightSource.addFeature(feature)
      } catch (error) {
        console.error('解析GeoJSON失败:', error)
      }
    }

    // 如果有extent边界框，使用fit自适应缩放；否则使用中心点定位
    if (extent && extent.length === 4) {
      const [minLon, minLat, maxLon, maxLat] = extent
      const extentInMapProj = [
        ...fromLonLat([minLon, minLat]),
        ...fromLonLat([maxLon, maxLat])
      ]

      // 自适应缩放到要素范围，添加适当的边距
      map.getView().fit(extentInMapProj, {
        padding: [100, 100, 100, 100],  // 四周留100像素边距
        duration: 300,                   // 动画时长
        maxZoom: 18                      // 最大缩放级别，避免点要素放得太大
      })
    } else if (centroid) {
      // 没有extent时，使用中心点定位
      const { lon, lat } = centroid
      const center = fromLonLat([lon, lat])
      map.getView().animate({ center, zoom: 16, duration: 300 })
    }
  }

  /**
   * 清除高亮
   */
  function clearHighlight() {
    if (highlightSource) {
      highlightSource.clear()
    }
  }

  return {
    highlightSource,
    highlightLayer,
    createHighlightLayer,
    focusFeatureById,
    clearHighlight
  }
}
