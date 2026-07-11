/**
 * 地图样式配置工具
 */
import Style from 'ol/style/Style.js'
import Fill from 'ol/style/Fill.js'
import Stroke from 'ol/style/Stroke.js'
import CircleStyle from 'ol/style/Circle.js'

/**
 * 创建默认的矢量要素样式函数
 * @returns {Function} OpenLayers 样式函数
 */
export function createDefaultStyleFunction() {
  return (feature, resolution) => {
    const geomType = feature.getGeometry()?.getType?.() || ''

    if (geomType.includes('Point')) {
      return new Style({
        image: new CircleStyle({
          radius: 4,
          fill: new Fill({ color: 'rgba(0, 153, 255, 0.8)' })
        })
      })
    } else if (geomType.includes('Line')) {
      return new Style({
        stroke: new Stroke({ color: '#ff5722', width: 2 })
      })
    }

    const polygonStrokeWidth = resolution > 40 ? 0.65 : (resolution > 10 ? 1 : 1.5)
    const polygonStrokeColor = resolution > 40 ? 'rgba(230, 81, 0, 0.72)' : '#E65100'

    // 面要素: 显示边框 + 透明填充（透明填充使点击检测可以覆盖整个面）
    return new Style({
      fill: new Fill({ color: 'rgba(0, 0, 0, 0.01)' }),  // 几乎透明的填充，用于点击检测
      stroke: new Stroke({ color: polygonStrokeColor, width: polygonStrokeWidth })
    })
  }
}

/**
 * 创建高亮图层样式
 * @returns {Style} OpenLayers Style 对象
 */
export function createHighlightStyle() {
  return new Style({
    fill: new Fill({ color: 'rgba(255, 255, 0, 0.2)' }),  // 黄色半透明填充
    stroke: new Stroke({ color: '#FFD700', width: 3 }),   // 金色边框
    image: new CircleStyle({
      radius: 8,
      fill: new Fill({ color: 'rgba(255, 215, 0, 0.8)' }),
      stroke: new Stroke({ color: '#FFD700', width: 2 })
    })
  })
}
