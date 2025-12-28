<template>
  <div ref="mapEl" class="vt-map"></div>
  <div v-if="error" class="vt-error">{{ error }}</div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import TileLayer from 'ol/layer/Tile.js'
import XYZ from 'ol/source/XYZ.js'
import VectorTileLayer from 'ol/layer/VectorTile.js'
import VectorTileSource from 'ol/source/VectorTile.js'
import MVT from 'ol/format/MVT.js'
import Style from 'ol/style/Style.js'
import Fill from 'ol/style/Fill.js'
import Stroke from 'ol/style/Stroke.js'
import CircleStyle from 'ol/style/Circle.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { defaults as defaultControls } from 'ol/control.js'
import client from '@/api/client'

const props = defineProps({
  resourceId: { type: [Number, String], required: true },
  schema: { type: String, required: true },
  table: { type: String, required: true },
  geom: { type: String, default: 'geom' },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 }
})

const mapEl = ref(null)
let map
const error = ref('')
const tileConfig = ref(null) // 瓦片配置（从后端获取）
const isLoadingConfig = ref(false) // 防止重复加载
let lastWarningZoom = null // 避免重复提示

// ✅ 跟踪所有进行中的瓦片请求 (使用 let 以便在闭包中正确引用)
let activeTileRequests = new Map()  // key: "z/x/y", value: AbortController

const apiBase = computed(() => client.defaults.baseURL)
const token = () => localStorage.getItem('token') || ''

// ✅ 构建瓦片唯一标识
function buildTileKey(z, x, y) {
  return `${z}/${x}/${y}`
}

// 获取瓦片配置（MinZoom/MaxZoom）
async function fetchTileConfig() {
  // 防止重复请求
  if (isLoadingConfig.value || tileConfig.value) {
    return
  }

  isLoadingConfig.value = true

  try {
    const url = `/engines/${props.engineId}/spatial/${props.schema}/${props.table}/tile-config`
    console.log('Fetching tile config from:', url)
    const response = await client.get(url)
    tileConfig.value = response.data
    console.log('Tile config loaded:', tileConfig.value)
  } catch (err) {
    console.warn('Failed to load tile config, using defaults:', err)
    // 失败时使用默认配置
    tileConfig.value = { min_zoom: 6, max_zoom: 18 }
  } finally {
    isLoadingConfig.value = false
  }
}

const tilesURLTemplate = computed(() => {
  const base = apiBase.value.replace(/\/$/, '')
  let path = `${base}/engines/${props.engineId}/spatial/tiles/${props.schema}/${props.table}/{z}/{x}/{y}`

  // 只传递非默认的几何列名
  if (props.geom && props.geom !== 'geom') {
    path += `?geom=${encodeURIComponent(props.geom)}`
  }

  return path
})

function makeVectorLayer() {
  const vtSource = new VectorTileSource({
    format: new MVT(),
    cacheSize: 512,  // 增加客户端缓存,减少重复请求
    maxZoom: 20,  // 支持更高层级的瓦片请求
    url: tilesURLTemplate.value,  // ✅ 使用 URL 模板（OpenLayers 会自动替换 {z}/{x}/{y}）
    // ✅ 自定义 tile loader 实现 AbortController
    tileLoadFunction: (tile, src) => {
      // ✅ 提取瓦片坐标
      const match = src.match(/\/tiles\/[^/]+\/[^/]+\/(\d+)\/(\d+)\/(\d+)/)
      if (!match) {
        console.warn('无法解析瓦片URL:', src)
        tile.setState(3)
        return
      }

      const z = parseInt(match[1], 10)
      const x = parseInt(match[2], 10)
      const y = parseInt(match[3], 10)
      const tileKey = buildTileKey(z, x, y)

      // ✅ 取消该瓦片之前未完成的请求
      if (activeTileRequests.has(tileKey)) {
        const oldController = activeTileRequests.get(tileKey)
        oldController.abort()
        console.debug('取消旧瓦片请求:', tileKey)
      }

      // ✅ 创建新的 AbortController
      const controller = new AbortController()
      activeTileRequests.set(tileKey, controller)

      // ✅ 发起请求 (关联取消信号)
      fetch(src, {
        headers: { Authorization: token() ? `Bearer ${token()}` : '' },
        signal: controller.signal
      })
        .then(res => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
          return res.arrayBuffer()
        })
        .then(buf => {
          const format = tile.getFormat() || new MVT()
          const features = format.readFeatures(buf, {
            extent: tile.getExtent(),
            featureProjection: tile.getProjection()
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
          tile.setState(3) // 设置为错误状态
        })
        .finally(() => {
          activeTileRequests.delete(tileKey)
        })
    }
  })

  const styleFn = (feature) => {
    const geomType = feature.getGeometry()?.getType?.() || ''
    if (geomType.includes('Point')) {
      return new Style({
        image: new CircleStyle({ radius: 4, fill: new Fill({ color: 'rgba(0, 153, 255, 0.8)' }) })
      })
    } else if (geomType.includes('Line')) {
      return new Style({ stroke: new Stroke({ color: '#ff5722', width: 2 }) })
    }
    // 面要素: 只显示边框,不填充(彻底不遮挡底图)
    return new Style({
      stroke: new Stroke({ color: '#E65100', width: 1.5 })
    })
  }

  return new VectorTileLayer({ source: vtSource, style: styleFn })
}

async function initMap() {
  // 1. 先获取瓦片配置
  await fetchTileConfig()

  // 2. 计算地图初始中心点和缩放级别
  let initialCenter = props.center
  let initialZoom = props.zoom

  // 如果后端返回了 extent，使用它来计算中心点
  if (tileConfig.value.extent && tileConfig.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = tileConfig.value.extent
    initialCenter = [(minX + maxX) / 2, (minY + maxY) / 2]

    // min_zoom 是后端算法计算的最佳初始视图层级
    // 在此层级，数据占据视口约 50%（实际约 35% 因向下取整），留有适当边距，已是"全幅显示"
    initialZoom = tileConfig.value.min_zoom || props.zoom

    console.log('Using extent-based center:', initialCenter, 'initial zoom:', initialZoom)
  } else {
    console.log('No extent available, using default center:', initialCenter)
  }

  // 3. 使用高德地图底图 - 正确的多URL配置
  const base = new TileLayer({
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

  const vt = makeVectorLayer()
  vt.setZIndex(10)  // MVT图层在上层

  // 4. 创建地图（移除 zoom 硬限制）
  map = new Map({
    target: mapEl.value,
    layers: [base, vt],
    maxTilesLoading: 16,  // 增加并发加载瓦片数,优化加载速度
    interactions: defaultInteractions({ mouseWheelZoom: false }).extend([
      new MouseWheelZoom({
        duration: 100,  // 缩放动画时长(毫秒) - 缩短以提升响应速度
        timeout: 100,   // 滚轮事件延迟 - 增大合并窗口，让连续滚动更流畅
        useAnchor: true // 以鼠标位置为中心缩放，交互更自然
      })
    ]),
    controls: defaultControls({
      zoom: true,
      zoomOptions: {
        zoomInTipLabel: '放大',
        zoomOutTipLabel: '缩小'
      }
    }),
    view: new View({
      center: fromLonLat(initialCenter),
      zoom: initialZoom,
      maxZoom: 20,  // 固定最大值，不限制用户
      minZoom: 1,   // 固定最小值，不限制用户
      smoothResolutionConstraint: true,     // 启用平滑分辨率，允许非整数zoom，缩放更流畅
      constrainResolution: false,           // 允许非整数缩放级别（如12.5），提升缩放体验
      enableRotation: false                 // 禁用地图旋转，避免误操作
    })
  })

  // 监听 zoom 变化，显示超出范围提示
  map.getView().on('change:resolution', () => {
    const currentZoom = Math.round(map.getView().getZoom())

    // 避免在同一 zoom 重复提示
    if (currentZoom === lastWarningZoom) return

    if (currentZoom < tileConfig.value.min_zoom) {
      ElMessage.warning({
        message: `当前层级 ${currentZoom} 低于建议范围，数据可能不可见。建议放大到 ${tileConfig.value.min_zoom} 层级`,
        duration: 3000,
        showClose: true
      })
      lastWarningZoom = currentZoom
    } else if (currentZoom > tileConfig.value.max_zoom) {
      ElMessage.info({
        message: `当前层级 ${currentZoom} 超出预缓存范围 (${tileConfig.value.max_zoom})，加载可能较慢`,
        duration: 3000,
        showClose: true
      })
      lastWarningZoom = currentZoom
    } else {
      // 回到正常范围，清除警告状态
      lastWarningZoom = null
    }
  })

  console.log(`Map zoom range (suggested): ${tileConfig.value.min_zoom} - ${tileConfig.value.max_zoom}, actual: 1 - 20`)
}

function fromLonLat(lonLat) {
  const [lon, lat] = lonLat
  const x = (lon * 20037508.34) / 180
  let y = Math.log(Math.tan(((90 + lat) * Math.PI) / 360)) / (Math.PI / 180)
  y = (y * 20037508.34) / 180
  return [x, y]
}

function focusFeatureById(featureId, centroid) {
  if (!map || !centroid) return
  const { lon, lat } = centroid
  const center = fromLonLat([lon, lat])
  map.getView().animate({ center, zoom: 16, duration: 300 })  // 优化动画时长(从500ms降到300ms)
}

defineExpose({ focusFeatureById })

onMounted(() => {
  initMap()

  // ✅ 监听地图移动,取消不同 zoom 的请求
  setTimeout(() => {
    if (map) {
      map.on('movestart', () => {
        const currentZoom = Math.round(map.getView().getZoom())

        // ✅ 防御性检查: 确保 activeTileRequests 是 Map
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
      })
    }
  }, 500) // 等待地图初始化完成
})

onBeforeUnmount(() => {
  // ✅ 组件卸载时清理所有请求
  if (activeTileRequests && activeTileRequests.forEach) {
    activeTileRequests.forEach((controller) => {
      controller.abort()
    })
    activeTileRequests.clear()
  }

  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.engineId, props.schema, props.table, props.geom], async () => {
  if (!map) return

  // 重置配置状态，允许重新加载
  tileConfig.value = null
  lastWarningZoom = null // 重置警告状态

  // 重新获取瓦片配置
  await fetchTileConfig()

  console.log(`Updated zoom range (suggested): ${tileConfig.value.min_zoom} - ${tileConfig.value.max_zoom}`)

  // 如果有 extent，更新地图中心点
  if (tileConfig.value.extent && tileConfig.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = tileConfig.value.extent
    const newCenter = [(minX + maxX) / 2, (minY + maxY) / 2]

    // min_zoom 本身就是最佳初始视图
    const view = map.getView()
    const newZoom = tileConfig.value.min_zoom || view.getZoom()

    view.setCenter(fromLonLat(newCenter))
    view.setZoom(newZoom)
    console.log('Updated map center to extent:', newCenter, 'zoom:', newZoom)
  }

  // 更新 MVT 图层
  const layers = map.getLayers()
  const vtIdx = layers.getLength() - 1
  const newVt = makeVectorLayer()
  newVt.setZIndex(10)
  layers.setAt(vtIdx, newVt)
})
</script>

<style scoped>
.vt-map { width: 100%; height: 100%; position: absolute; inset: 0; }
.vt-error { position: absolute; top: 8px; right: 8px; background: rgba(0,0,0,.6); color: #fff; padding: 6px 8px; border-radius: 4px; font-size: 12px; }
</style>
