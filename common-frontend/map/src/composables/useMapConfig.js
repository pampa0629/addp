import { ref, computed, onMounted } from 'vue'
import defaultConfigAPI from '../api/config'

const GAODE_BASE_MAP_VALUE = 'amapVector'

const BASE_MAP_PROFILES = {
  osm: {
    provider: 'osm',
    tile_matrix_set: 'xyz',
    view_crs: 'EPSG:3857',
    coordinate_policy: 'wgs84',
    attribution: '© OpenStreetMap contributors',
    network_policy: 'online'
  },
  tiandituVector: {
    provider: 'tianditu',
    tile_matrix_set: 'tianditu_w',
    view_crs: 'EPSG:3857',
    coordinate_policy: 'wgs84'
  },
  tiandituImage: {
    provider: 'tianditu',
    tile_matrix_set: 'tianditu_w',
    view_crs: 'EPSG:3857',
    coordinate_policy: 'wgs84'
  },
  [GAODE_BASE_MAP_VALUE]: {
    provider: 'amap',
    tile_matrix_set: 'xyz',
    view_crs: 'EPSG:3857',
    coordinate_policy: 'gcj02'
  }
}

// 允许宿主应用注入真实的 configAPI 实现
let configAPI = defaultConfigAPI

/**
 * 注入地图配置 API 实现（在 main.js 中调用，早于任何组件挂载）
 * @param {Object} api - 包含 getMapConfig() 方法的对象
 */
export function setMapConfigAPI(api) {
  configAPI = api
  // 重置加载状态，以便使用新的 API 重新加载
  isConfigLoaded = false
  baseMapOptions.value = []
  mapConfig.value = { amapKey: '', amapSecurityJsCode: '', tdtKey: '' }
}

// 全局共享的地图配置
const mapConfig = ref({
  amapKey: '',
  amapSecurityJsCode: '',
  tdtKey: ''
})

const baseMapOptions = ref([])

let isConfigLoaded = false

/**
 * 地图配置管理 Composable
 */
export function useMapConfig() {
  const ensureBaseMapOption = (value, label) => {
    const exists = baseMapOptions.value.some((item) => item.value === value)
    if (!exists) {
      baseMapOptions.value = [
        ...baseMapOptions.value,
        {
          label,
          value,
          profile: BASE_MAP_PROFILES[value] || null
        }
      ]
    }
  }

  const applyGaodeConfig = (amapKey, securityJsCode) => {
    if (!amapKey) return

    mapConfig.value = {
      ...mapConfig.value,
      amapKey,
      amapSecurityJsCode: securityJsCode || ''
    }

    if (securityJsCode && typeof window !== 'undefined') {
      window._AMapSecurityConfig = {
        ...(window._AMapSecurityConfig || {}),
        securityJsCode
      }
    }

    ensureBaseMapOption(GAODE_BASE_MAP_VALUE, '高德地图 矢量（GCJ-02）')
  }

  const applyTiandituConfig = (tdtKey) => {
    if (!tdtKey) return

    mapConfig.value = {
      ...mapConfig.value,
      tdtKey
    }

    ensureBaseMapOption('tiandituVector', '天地图 矢量')
    ensureBaseMapOption('tiandituImage', '天地图 影像')
  }

  const loadMapConfig = async () => {
    if (isConfigLoaded) return

    let amapKey = ''
    let securityJsCode = ''
    let tdtKey = ''

    try {
      const response = await configAPI.getMapConfig()
      const data = response.data || {}
      amapKey = data?.amap_key || ''
      securityJsCode = data?.amap_security_js_code || ''
      tdtKey = data?.tdt_key || ''
    } catch (error) {
      console.warn('加载地图配置失败，使用默认配置', error)
    }

    applyTiandituConfig(tdtKey)
    applyGaodeConfig(amapKey, securityJsCode)

    // OpenStreetMap does not require an application key and keeps local preview
    // usable when no managed online map service has been configured.
    if (baseMapOptions.value.length === 0) {
      ensureBaseMapOption('osm', 'OpenStreetMap')
    }

    isConfigLoaded = true
  }

  const defaultBaseMapType = computed(() => {
    if (baseMapOptions.value.length === 0) return ''
    return baseMapOptions.value[0].value
  })

  const getBaseMapProfile = (baseMapType) => {
    return BASE_MAP_PROFILES[baseMapType] || null
  }

  return {
    mapConfig,
    baseMapOptions,
    defaultBaseMapType,
    getBaseMapProfile,
    loadMapConfig,
    GAODE_BASE_MAP_VALUE
  }
}
