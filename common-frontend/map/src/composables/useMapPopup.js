/**
 * 地图Popup弹窗逻辑的组合式函数
 */
import { ref } from 'vue'
import Overlay from 'ol/Overlay.js'
import { formatFeatureProperties } from '../utils/mapFormatters.js'

/**
 * 创建和管理地图Popup
 * @param {Object} options - 配置选项
 * @param {string} options.geomColumn - 几何列名
 * @returns {Object} Popup相关的响应式数据和方法
 */
export function useMapPopup(options = {}) {
  const { geomColumn = 'geom', labels = {}, geometryTypeLabels = {} } = options

  const popupEl = ref(null)
  const popupContent = ref('')
  let popup = null

  /**
   * 创建Popup Overlay
   * @param {Map} map - OpenLayers Map实例
   */
  function createPopup(map) {
    if (!popupEl.value || !map) return

    popup = new Overlay({
      element: popupEl.value,
      autoPan: true,
      autoPanAnimation: {
        duration: 250
      },
      positioning: 'bottom-center',
      offset: [0, -10]
    })

    map.addOverlay(popup)
  }

  /**
   * 显示Popup
   * @param {Object} feature - OpenLayers Feature对象
   * @param {Array} coordinate - 坐标位置
   */
  function showPopup(feature, coordinate) {
    if (!popup) return

    const properties = feature.getProperties()
    popupContent.value = formatFeatureProperties(properties, {
      geomColumn,
      labels,
      geometryTypeLabels
    })
    popup.setPosition(coordinate)
  }

  /**
   * 关闭Popup
   */
  function closePopup() {
    if (popup) {
      popup.setPosition(undefined)
    }
    popupContent.value = ''
  }

  /**
   * 提取要素ID
   * @param {Object} properties - 要素属性对象
   * @returns {string|number|null} 要素ID
   */
  function extractFeatureId(properties) {
    return properties.id || properties.ID || properties._id || properties.uuid || null
  }

  return {
    popupEl,
    popupContent,
    createPopup,
    showPopup,
    closePopup,
    extractFeatureId
  }
}
