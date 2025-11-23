<template>
  <div ref="mapEl" class="vt-map"></div>
  <div v-if="error" class="vt-error">{{ error }}</div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch, computed } from 'vue'
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
import client from '@/api/client'

const props = defineProps({
  resourceId: { type: [Number, String], required: true },
  schema: { type: String, required: true },
  table: { type: String, required: true },
  geom: { type: String, default: 'geom' },
  cols: { type: Array, default: () => ['id'] },
  srid: { type: Number, default: 4326 },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 }
})

const mapEl = ref(null)
let map
const error = ref('')

const apiBase = computed(() => client.defaults.baseURL)
const token = () => localStorage.getItem('token') || ''

const tilesURLTemplate = computed(() => {
  const base = apiBase.value.replace(/\/$/, '')

  // 新的 RESTful API 格式：
  // /api/resources/{id}/spatial/tiles/{schema}/{table}/{z}/{x}/{y}.mvt
  let path = `${base}/resources/${props.resourceId}/spatial/tiles/${props.schema}/${props.table}/{z}/{x}/{y}.mvt`

  // 添加可选查询参数
  const params = []
  if (props.geom && props.geom !== 'geom') {
    params.push(`geom=${encodeURIComponent(props.geom)}`)
  }
  if (props.srid && props.srid !== 4326) {
    params.push(`srid=${props.srid}`)
  }
  if (props.cols?.length) {
    params.push(`cols=${props.cols.map(c => encodeURIComponent(c)).join(',')}`)
  }

  return params.length > 0 ? `${path}?${params.join('&')}` : path
})

function makeVectorLayer() {
  const vtSource = new VectorTileSource({
    format: new MVT(),
    tileUrlFunction: (tileCoord) => {
      // OpenLayers 使用 [z, x, -y-1] 格式（TMS）
      // 需要转换为标准 XYZ 格式: y = 2^z - y - 1
      const z = tileCoord[0]
      const x = tileCoord[1]
      const y = tileCoord[2]

      // 转换 TMS Y 坐标到 XYZ 格式
      const xyzY = Math.pow(2, z) - y - 1

      const url = tilesURLTemplate.value
        .replace('{z}', z)
        .replace('{x}', x)
        .replace('{y}', xyzY)

      return url
    },
    tileLoadFunction: async (tile, src) => {
      try {
        const res = await fetch(src, {
          headers: {
            Authorization: token() ? `Bearer ${token()}` : ''
          }
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const buf = await res.arrayBuffer()
        tile.setLoader((extent, resolution, projection) => {
          const format = tile.getFormat() || new MVT()
          const features = format.readFeatures(buf, { extent, featureProjection: projection })
          tile.setFeatures(features)
          tile.setProjection(projection)
        })
      } catch (e) {
        error.value = `加载切片失败: ${e?.message || e}`
        tile.setState(3) // ERROR
      }
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
    return new Style({
      fill: new Fill({ color: 'rgba(0, 136, 136, 0.4)' }),
      stroke: new Stroke({ color: '#008888', width: 1 })
    })
  }

  return new VectorTileLayer({ source: vtSource, style: styleFn })
}

function initMap() {
  const base = new TileLayer({
    source: new XYZ({ url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png', crossOrigin: 'anonymous' })
  })
  const vt = makeVectorLayer()
  map = new Map({
    target: mapEl.value,
    layers: [base, vt],
    view: new View({ center: fromLonLat(props.center), zoom: props.zoom })
  })
}

function fromLonLat(lonLat) {
  // minimal transform for WebMercator; OpenLayers expects EPSG:3857
  const [lon, lat] = lonLat
  const x = (lon * 20037508.34) / 180
  let y = Math.log(Math.tan(((90 + lat) * Math.PI) / 360)) / (Math.PI / 180)
  y = (y * 20037508.34) / 180
  return [x, y]
}

// 通过 ID 定位要素（用于表格行点击后定位到地图）
function focusFeatureById(featureId, centroid) {
  if (!map || !centroid) return

  const { lon, lat } = centroid
  const center = fromLonLat([lon, lat])

  // 平移地图到要素中心点，并适当放大
  map.getView().animate({
    center,
    zoom: 16, // 放大到合适的缩放级别
    duration: 500
  })
}

// 暴露方法给父组件调用
defineExpose({
  focusFeatureById
})

onMounted(() => initMap())
onBeforeUnmount(() => {
  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.resourceId, props.schema, props.table, props.geom, props.cols?.join(','), props.srid], () => {
  // Recreate vector layer on source change
  if (!map) return
  const layers = map.getLayers()
  const vtIdx = layers.getLength() - 1
  layers.setAt(vtIdx, makeVectorLayer())
})
</script>

<style scoped>
.vt-map { width: 100%; height: 100%; position: absolute; inset: 0; }
.vt-error { position: absolute; top: 8px; right: 8px; background: rgba(0,0,0,.6); color: #fff; padding: 6px 8px; border-radius: 4px; font-size: 12px; }
</style>
