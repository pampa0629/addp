<template>
  <div ref="mapEl" class="tile-preview-map"></div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import TileLayer from 'ol/layer/Tile.js'
import VectorTileLayer from 'ol/layer/VectorTile.js'
import XYZ from 'ol/source/XYZ.js'
import VectorTileSource from 'ol/source/VectorTile.js'
import MVT from 'ol/format/MVT.js'
import { fromLonLat, transformExtent } from 'ol/proj.js'
import { createDefaultStyleFunction } from '../utils/mapStyles.js'

const props = defineProps({
  // 瓦片 URL 模板，如：http://localhost:8000/tiles/{serviceName}/{layerName}/{z}/{x}/{y}.mvt
  tileUrl: {
    type: String,
    required: true
  },
  // 底图类型：'osm' | 'gaode' | 'none'
  baseMap: {
    type: String,
    default: 'none'
  },
  // 初始中心点 [经度, 纬度]
  center: {
    type: Array,
    default: () => [116.4, 39.9]
  },
  // 初始全幅范围 [minLon, minLat, maxLon, maxLat]，有效时优先于 center/zoom
  extent: {
    type: Array,
    default: null
  },
  // 初始缩放级别
  zoom: {
    type: Number,
    default: 10
  },
  // 最小缩放级别
  minZoom: {
    type: Number,
    default: 0
  },
  // 最大缩放级别
  maxZoom: {
    type: Number,
    default: 22
  }
})

const emit = defineEmits(['tileLoadStart', 'tileLoadEnd', 'tileLoadError'])

const mapEl = ref(null)
let map = null

function normalizedExtent() {
  if (!Array.isArray(props.extent) || props.extent.length !== 4) return null
  const extent = props.extent.map(Number)
  if (!extent.every(Number.isFinite)) return null
  if (extent[0] < -180 || extent[2] > 180 || extent[1] < -90 || extent[3] > 90) return null
  if (extent[0] > extent[2] || extent[1] > extent[3]) return null
  return extent
}

function fitInitialExtent() {
  const extent = normalizedExtent()
  if (!map || !extent) return
  map.updateSize()
  const size = map.getSize()
  if (!size || size.some(value => !Number.isFinite(value) || value <= 0)) return
  map.getView().fit(transformExtent(extent, 'EPSG:4326', 'EPSG:3857'), {
    size,
    padding: [48, 48, 48, 48],
    maxZoom: props.maxZoom
  })
}

// 创建底图图层
function createBaseLayer() {
  if (props.baseMap === 'none') {
    return null
  }

  if (props.baseMap === 'osm') {
    return new TileLayer({
      source: new XYZ({
        url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
        attributions: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
      })
    })
  }

  if (props.baseMap === 'gaode') {
    console.warn('高德地图底图暂未在 TilePreview 中实现，已使用无底图模式')
    return null
  }

  return null
}

// 初始化地图
function initMap() {
  if (!mapEl.value) {
    console.error('地图容器未找到')
    return
  }

  // 创建图层
  const layers = []

  const baseLayer = createBaseLayer()
  if (baseLayer) {
    layers.push(baseLayer)
  }

  // 创建 VectorTile 图层
  const vectorTileLayer = new VectorTileLayer({
    source: new VectorTileSource({
      format: new MVT(),
      url: props.tileUrl
    }),
    style: createDefaultStyleFunction()
  })

  // 监听瓦片加载事件
  vectorTileLayer.getSource().on('tileloadstart', () => {
    console.log('[TilePreview] 瓦片开始加载')
    emit('tileLoadStart')
  })

  vectorTileLayer.getSource().on('tileloadend', (evt) => {
    console.log('[TilePreview] 瓦片加载成功', evt.tile)
    emit('tileLoadEnd', evt.tile)
  })

  vectorTileLayer.getSource().on('tileloaderror', (evt) => {
    console.error('[TilePreview] 瓦片加载失败', evt.tile)
    emit('tileLoadError', evt.tile)
  })

  layers.push(vectorTileLayer)

  // 创建地图
  map = new Map({
    target: mapEl.value,
    layers: layers,
    view: new View({
      center: fromLonLat(props.center),
      zoom: props.zoom,
      minZoom: props.minZoom,
      maxZoom: props.maxZoom
    })
  })

  fitInitialExtent()

  console.log('[TilePreview] 地图已初始化', {
    tileUrl: props.tileUrl,
    baseMap: props.baseMap,
    center: props.center,
    zoom: props.zoom,
    extent: normalizedExtent()
  })
}

// 数据源或初始视角变化时重新初始化；普通容器尺寸变化不会重置用户视角。
watch(() => [props.tileUrl, props.extent, props.center, props.zoom, props.minZoom, props.maxZoom], () => {
  if (map) {
    map.setTarget(null)
    map = null
  }
  initMap()
}, { deep: true })

onMounted(() => {
  initMap()
})

onBeforeUnmount(() => {
  if (map) {
    map.setTarget(null)
    map = null
  }
})
</script>

<style scoped>
.tile-preview-map {
  width: 100%;
  height: 100%;
  min-height: 400px;
}
</style>
