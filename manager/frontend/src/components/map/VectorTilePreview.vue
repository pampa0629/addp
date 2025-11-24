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
  let path = `${base}/resources/${props.resourceId}/spatial/tiles/${props.schema}/${props.table}/{z}/{x}/{y}`

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
    tileLoadFunction: (tile, src) => {
      // 非阻塞加载
      fetch(src, {
        headers: { Authorization: token() ? `Bearer ${token()}` : '' }
      }).then(res => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.arrayBuffer()
      }).then(buf => {
        tile.setLoader((extent, resolution, projection) => {
          const format = tile.getFormat() || new MVT()
          const features = format.readFeatures(buf, { extent, featureProjection: projection })
          tile.setFeatures(features)
          tile.setProjection(projection)
        })
      }).catch(e => {
        console.error('加载切片失败:', src, e)
        tile.setState(3)
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

function initMap() {
  // 使用高德地图底图 - 正确的多URL配置
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

  map = new Map({
    target: mapEl.value,
    layers: [base, vt],
    view: new View({
      center: fromLonLat(props.center),
      zoom: props.zoom,
      maxZoom: 18,
      minZoom: 3
    })
  })
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
  map.getView().animate({ center, zoom: 16, duration: 500 })
}

defineExpose({ focusFeatureById })

onMounted(() => initMap())
onBeforeUnmount(() => {
  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.resourceId, props.schema, props.table, props.geom, props.cols?.join(','), props.srid], () => {
  if (!map) return
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
