<template>
  <div ref="mapEl" class="geojson-map"></div>
  <div v-if="error" class="geojson-error">{{ error }}</div>
  <div v-if="loading" class="geojson-loading">加载中...</div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import TileLayer from 'ol/layer/Tile.js'
import XYZ from 'ol/source/XYZ.js'
import VectorLayer from 'ol/layer/Vector.js'
import VectorSource from 'ol/source/Vector.js'
import GeoJSON from 'ol/format/GeoJSON.js'
import Style from 'ol/style/Style.js'
import Fill from 'ol/style/Fill.js'
import Stroke from 'ol/style/Stroke.js'
import CircleStyle from 'ol/style/Circle.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { defaults as defaultControls } from 'ol/control.js'
import client from '@/api/client'

const props = defineProps({
  engineId: { type: [Number, String], required: true },
  schema: { type: String, required: true },
  table: { type: String, required: true },
  geom: { type: String, default: 'geom' },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 }
})

const mapEl = ref(null)
let map
const error = ref('')
const loading = ref(false)
const metadata = ref(null) // 存储元数据（count、extent、srid）
let vectorLayer = null
let vectorSource = null

const apiBase = computed(() => client.defaults.baseURL)

// 获取 GeoJSON 元数据
async function fetchMetadata() {
  try {
    const url = `/engines/${props.engineId}/spatial/${props.schema}/${props.table}/geojson/metadata`
    const params = { geom_column: props.geom }
    const response = await client.get(url, { params })
    metadata.value = response.data
    console.log('GeoJSON metadata loaded:', metadata.value)
  } catch (err) {
    console.warn('Failed to load GeoJSON metadata:', err)
    metadata.value = null
  }
}

// 加载 GeoJSON 数据（支持分页）
async function loadGeoJSON(page = 1, pageSize = 1000) {
  if (loading.value) return

  loading.value = true
  error.value = ''

  try {
    const url = `/engines/${props.engineId}/spatial/${props.schema}/${props.table}/geojson`
    const params = {
      page,
      page_size: pageSize,
      geom_column: props.geom
    }

    console.log('Loading GeoJSON from:', url, params)
    const response = await client.get(url, { params })

    // 使用 OpenLayers GeoJSON 格式解析
    const format = new GeoJSON()
    const features = format.readFeatures(response.data, {
      dataProjection: 'EPSG:4326',  // GeoJSON 默认使用 WGS84
      featureProjection: 'EPSG:3857' // Web Mercator (地图投影)
    })

    console.log(`Loaded ${features.length} features from GeoJSON`)

    // 添加要素到图层
    if (vectorSource) {
      if (page === 1) {
        vectorSource.clear() // 首页清空旧数据
      }
      vectorSource.addFeatures(features)
    }

    // 如果有数据且是首页，自动缩放到数据范围
    if (page === 1 && features.length > 0 && map) {
      const extent = vectorSource.getExtent()
      map.getView().fit(extent, {
        padding: [50, 50, 50, 50],
        maxZoom: 16,
        duration: 500
      })
    }

  } catch (err) {
    console.error('Failed to load GeoJSON:', err)
    error.value = '加载 GeoJSON 数据失败'
    ElMessage.error('加载数据失败: ' + (err.response?.data?.error || err.message))
  } finally {
    loading.value = false
  }
}

// 样式函数（与 VectorTilePreview 保持一致）
function styleFunction(feature) {
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
  // 面要素: 只显示边框
  return new Style({
    stroke: new Stroke({ color: '#E65100', width: 1.5 })
  })
}

function fromLonLat(lonLat) {
  const [lon, lat] = lonLat
  const x = (lon * 20037508.34) / 180
  let y = Math.log(Math.tan(((90 + lat) * Math.PI) / 360)) / (Math.PI / 180)
  y = (y * 20037508.34) / 180
  return [x, y]
}

async function initMap() {
  // 1. 先获取元数据
  await fetchMetadata()

  // 2. 计算地图初始中心点和缩放级别
  let initialCenter = props.center
  let initialZoom = props.zoom

  // 如果后端返回了 extent，使用它来计算中心点
  if (metadata.value?.extent && metadata.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = metadata.value.extent
    initialCenter = [(minX + maxX) / 2, (minY + maxY) / 2]

    // 根据数据范围计算合适的缩放级别
    const lonRange = maxX - minX
    const latRange = maxY - minY
    const maxRange = Math.max(lonRange, latRange)

    // 简单的缩放级别估算
    if (maxRange > 10) initialZoom = 6
    else if (maxRange > 5) initialZoom = 8
    else if (maxRange > 1) initialZoom = 10
    else if (maxRange > 0.1) initialZoom = 12
    else initialZoom = 14

    console.log('Using metadata extent:', initialCenter, 'zoom:', initialZoom)
  }

  // 3. 创建高德底图
  const baseLayer = new TileLayer({
    source: new XYZ({
      urls: [
        'https://webrd01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd02.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd03.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd04.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}'
      ],
      crossOrigin: 'anonymous'
    }),
    zIndex: 0
  })

  // 4. 创建 Vector Layer
  vectorSource = new VectorSource()
  vectorLayer = new VectorLayer({
    source: vectorSource,
    style: styleFunction,
    zIndex: 10
  })

  // 5. 创建地图
  map = new Map({
    target: mapEl.value,
    layers: [baseLayer, vectorLayer],
    interactions: defaultInteractions({ mouseWheelZoom: false }).extend([
      new MouseWheelZoom({
        duration: 100,
        timeout: 100,
        useAnchor: true
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
      maxZoom: 20,
      minZoom: 1,
      smoothResolutionConstraint: true,
      constrainResolution: false,
      enableRotation: false
    })
  })

  // 6. 加载 GeoJSON 数据
  await loadGeoJSON(1, 1000)
}

function focusFeatureById(featureId, centroid) {
  if (!map || !centroid) return
  const { lon, lat } = centroid
  const center = fromLonLat([lon, lat])
  map.getView().animate({ center, zoom: 16, duration: 300 })
}

defineExpose({ focusFeatureById, loadGeoJSON })

onMounted(() => {
  initMap()
})

onBeforeUnmount(() => {
  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.engineId, props.schema, props.table, props.geom], async () => {
  if (!map) return

  // 重新加载数据
  metadata.value = null
  await fetchMetadata()

  // 如果有新的 extent，更新地图中心
  if (metadata.value?.extent && metadata.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = metadata.value.extent
    const newCenter = [(minX + maxX) / 2, (minY + maxY) / 2]
    map.getView().setCenter(fromLonLat(newCenter))
  }

  // 重新加载 GeoJSON
  await loadGeoJSON(1, 1000)
})
</script>

<style scoped>
.geojson-map {
  width: 100%;
  height: 100%;
  position: absolute;
  inset: 0;
}
.geojson-error {
  position: absolute;
  top: 8px;
  right: 8px;
  background: rgba(220, 38, 38, 0.9);
  color: #fff;
  padding: 6px 12px;
  border-radius: 4px;
  font-size: 12px;
  z-index: 1000;
}
.geojson-loading {
  position: absolute;
  top: 8px;
  left: 8px;
  background: rgba(59, 130, 246, 0.9);
  color: #fff;
  padding: 6px 12px;
  border-radius: 4px;
  font-size: 12px;
  z-index: 1000;
}
</style>
