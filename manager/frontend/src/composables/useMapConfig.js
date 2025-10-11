import { ref, computed, onMounted } from 'vue'
import configAPI from '../api/config'

const DEFAULT_AMAP_KEY = import.meta.env.VITE_AMAP_KEY || ''
const DEFAULT_AMAP_SECURITY = import.meta.env.VITE_AMAP_SECURITY || ''
const DEFAULT_TDT_KEY = import.meta.env.VITE_TDT_KEY || ''

const GAODE_BASE_MAP_VALUE = 'amapVector'

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
      baseMapOptions.value = [...baseMapOptions.value, { label, value }]
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

    ensureBaseMapOption(GAODE_BASE_MAP_VALUE, '高德地图 矢量')
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

    // 使用环境变量作为后备
    if (!amapKey && DEFAULT_AMAP_KEY) {
      amapKey = DEFAULT_AMAP_KEY
      if (!securityJsCode && DEFAULT_AMAP_SECURITY) {
        securityJsCode = DEFAULT_AMAP_SECURITY
      }
    }

    if (!tdtKey && DEFAULT_TDT_KEY) {
      tdtKey = DEFAULT_TDT_KEY
    }

    applyTiandituConfig(tdtKey)
    applyGaodeConfig(amapKey, securityJsCode)

    isConfigLoaded = true
  }

  const defaultBaseMapType = computed(() => {
    if (baseMapOptions.value.length === 0) return ''
    return baseMapOptions.value[0].value
  })

  return {
    mapConfig,
    baseMapOptions,
    defaultBaseMapType,
    loadMapConfig,
    GAODE_BASE_MAP_VALUE
  }
}
