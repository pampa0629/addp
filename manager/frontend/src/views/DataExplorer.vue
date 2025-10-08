<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <div class="tree-panel">
        <el-card shadow="never">
          <template #header>
            <div class="panel-header">
              <span>存储引擎</span>
              <el-button size="small" :loading="loadingTree" @click="loadTree">
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
          </template>
          <el-tree
            :data="treeData"
            :props="treeProps"
            node-key="id"
            :highlight-current="true"
            :expand-on-click-node="false"
            @node-click="handleNodeClick"
            v-loading="loadingTree"
          >
            <template #default="{ data }">
              <span class="tree-node" :class="data.type">
                <el-icon v-if="data.type === 'resource'"><Collection /></el-icon>
                <el-icon v-else-if="data.type === 'schema'"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span class="label" :title="data.label">{{ data.label }}</span>
              </span>
            </template>
          </el-tree>
        </el-card>
      </div>

      <div class="splitter" @mousedown="startDrag"></div>

      <div class="preview-wrapper">
        <el-card shadow="never" class="preview-panel">
          <template #header>
            <div class="panel-header">
              <span v-if="selectedTableLabel">{{ selectedTableLabel }} - 数据预览</span>
              <span v-else>请选择一张表</span>
              <div v-if="hasGeometry" class="map-controls">
                <div class="toggle-wrapper">
                  <span>地图预览</span>
                  <el-switch v-model="showMap" size="small" />
                </div>
                <el-select v-model="baseMapType" size="small" class="base-map-select">
                  <el-option
                    v-for="item in baseMapOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value"
                  />
                </el-select>
              </div>
            </div>
          </template>

          <div v-if="!selectedTable" class="empty-state">
            <el-empty description="从左侧选择一张表查看数据" />
          </div>

          <div v-else class="preview-content">
            <div
              v-if="hasGeometry && showMap"
              class="map-container"
              ref="mapContainer"
              :style="{ height: mapHeight + 'px' }"
            ></div>
            <div
              v-if="hasGeometry && showMap"
              class="map-table-splitter"
              @mousedown="startMapResize"
            ></div>
            <div class="table-wrapper">
              <el-table
                ref="tableRef"
                :data="preview.rows"
                v-loading="loadingPreview"
                height="100%"
                highlight-current-row
                :row-key="getRowKey"
                :current-row-key="currentRowKey"
                @row-click="handleRowClick"
              >
                <el-table-column
                  v-for="col in preview.columns"
                  :key="col"
                  :prop="col"
                  :label="col"
                  show-overflow-tooltip
                />
              </el-table>
            </div>
            <div class="pagination" v-if="preview.total > 0">
              <el-pagination
                background
                layout="prev, pager, next"
                :total="preview.total"
                :page-size="pageSize"
                :current-page="currentPage"
                @current-change="handlePageChange"
              />
              <div class="tip">最多展示前 50 行数据</div>
            </div>
          </div>
        </el-card>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Folder, Collection, Document } from '@element-plus/icons-vue'
import dataExplorerAPI from '../api/dataExplorer'
import configAPI from '../api/config'
import AMapLoader from '@amap/amap-jsapi-loader'
import OlMap from 'ol/Map'
import OlView from 'ol/View'
import TileLayer from 'ol/layer/Tile'
import VectorLayer from 'ol/layer/Vector'
import XYZ from 'ol/source/XYZ'
import VectorSource from 'ol/source/Vector'
import GeoJSON from 'ol/format/GeoJSON'
import Overlay from 'ol/Overlay.js'
import { unByKey } from 'ol/Observable.js'
import { fromLonLat, toLonLat } from 'ol/proj'
import Style from 'ol/style/Style'
import Fill from 'ol/style/Fill'
import Stroke from 'ol/style/Stroke'
import CircleStyle from 'ol/style/Circle'

const DEFAULT_AMAP_KEY = import.meta.env.VITE_AMAP_KEY || ''
const DEFAULT_AMAP_SECURITY = import.meta.env.VITE_AMAP_SECURITY || ''
const DEFAULT_TDT_KEY = import.meta.env.VITE_TDT_KEY || ''

const treeData = ref([])
const treeProps = {
  label: 'label',
  children: 'children'
}
const loadingTree = ref(false)

const selectedTable = ref(null)
const selectedTableLabel = computed(() => {
  if (!selectedTable.value) return ''
  return `${selectedTable.value.schema}.${selectedTable.value.table}`
})

const preview = reactive({
  columns: [],
  rows: [],
  total: 0
})
const loadingPreview = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const geometryColumns = ref([])
const showMap = ref(true)
const mapContainer = ref(null)
const mapConfig = ref({
  amapKey: '',
  amapSecurityJsCode: '',
  tdtKey: ''
})

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')
const mapHeight = ref(260)
const minMapHeight = 140
const maxMapHeight = 520

const DEFAULT_CENTER = [104.0668, 30.5728]

const baseMapOptions = ref([])
const baseMapType = ref('')
const GAODE_BASE_MAP_VALUE = 'amapVector'
const tableRef = ref(null)
const currentRowKey = ref('')
const rowOverlayMap = new Map()
const rowFeatureMap = new Map()
let gaodeInfoWindow = null
let olPopupOverlay = null
let olPopupElement = null
let olMapClickKey = null
let mapViewState = { center: DEFAULT_CENTER, zoom: 4 }
let activeMapType = ''
let gaodeEventsBound = false
let olViewEventKeys = []
let isMapResizing = false
let mapResizeStartY = 0
let mapResizeStartHeight = 0

const ensureBaseMapOption = (value, label) => {
  const exists = baseMapOptions.value.some((item) => item.value === value)
  if (!exists) {
    baseMapOptions.value = [...baseMapOptions.value, { label, value }]
  }
}

const updateGaodeViewState = () => {
  if (!gaodeMapInstance) return
  const center = gaodeMapInstance.getCenter?.()
  const zoom = gaodeMapInstance.getZoom?.()
  if (center && isFinite(center.lng) && isFinite(center.lat) && isFinite(zoom)) {
    mapViewState = {
      center: [center.lng, center.lat],
      zoom
    }
  }
}

const updateOlViewState = () => {
  if (!olMap) return
  const view = olMap.getView?.()
  if (!view) return
  const center = view.getCenter?.()
  const zoom = view.getZoom?.()
  if (center && isFinite(zoom)) {
    const lonLat = toLonLat(center)
    if (lonLat && isFinite(lonLat[0]) && isFinite(lonLat[1])) {
      mapViewState = {
        center: lonLat,
        zoom
      }
    }
  }
}

const bindGaodeEvents = () => {
  if (!gaodeMapInstance || gaodeEventsBound || !gaodeMapInstance.on) return
  gaodeMapInstance.on('moveend', updateGaodeViewState)
  gaodeMapInstance.on('zoomend', updateGaodeViewState)
  gaodeEventsBound = true
}

const bindOlEvents = () => {
  if (!olMap) return
  const view = olMap.getView?.()
  if (!view) return
  if (olViewEventKeys.length === 0) {
    olViewEventKeys.push(view.on('change:center', updateOlViewState))
    olViewEventKeys.push(view.on('change:resolution', updateOlViewState))
  }
}

const applyGaodeViewState = () => {
  if (!gaodeMapInstance) return
  const AMapModule = amapLib || window.AMap
  if (!AMapModule || !mapViewState?.center) return
  const [lng, lat] = mapViewState.center
  if (!isFinite(lng) || !isFinite(lat)) return
  const zoom = isFinite(mapViewState.zoom) ? mapViewState.zoom : 4
  gaodeMapInstance.setZoomAndCenter(zoom, new AMapModule.LngLat(lng, lat))
}

const applyOlViewState = () => {
  if (!olMap || !mapViewState?.center) return
  const view = olMap.getView?.()
  if (!view) return
  const [lng, lat] = mapViewState.center
  if (!isFinite(lng) || !isFinite(lat)) return
  const zoom = isFinite(mapViewState.zoom) ? mapViewState.zoom : 4
  view.setCenter(fromLonLat([lng, lat]))
  view.setZoom(zoom)
}

const captureViewState = () => {
  if (activeMapType === GAODE_BASE_MAP_VALUE) {
    updateGaodeViewState()
  } else if (activeMapType === 'tiandituVector' || activeMapType === 'tiandituImage') {
    updateOlViewState()
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
  if (!baseMapType.value) {
    baseMapType.value = GAODE_BASE_MAP_VALUE
  }
}

const applyTiandituConfig = (tdtKey) => {
  if (!tdtKey) return

  mapConfig.value = {
    ...mapConfig.value,
    tdtKey
  }

  ensureBaseMapOption('tiandituVector', '天地图 矢量')
  ensureBaseMapOption('tiandituImage', '天地图 影像')

  if (!baseMapType.value) {
    baseMapType.value = 'tiandituVector'
  }
}

const loadMapConfig = async () => {
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
    console.warn('加载地图配置失败', error)
  }

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
}

const treeWidth = ref(320)
const minTreeWidth = 220
const maxTreeWidth = 600
let startX = 0
let startWidth = 0
let isDragging = false
let amapLib = null
let gaodeMapInstance = null
let gaodeOverlays = []
let olMap = null
let olVectorLayer = null
let olVectorSource = null
let currentOlBaseType = ''

const geoFeatures = computed(() => {
  if (!hasGeometry.value || !activeGeometryColumn.value) return []
  const column = activeGeometryColumn.value
  return (preview.rows || [])
    .map((row) => {
      const geometryStr = row[column]
      if (!geometryStr) return null
      try {
        const geometry = typeof geometryStr === 'string' ? JSON.parse(geometryStr) : geometryStr
        return {
          type: 'Feature',
          geometry,
          properties: row
        }
      } catch (error) {
        console.warn('解析 GeoJSON 失败', error)
        return null
      }
    })
    .filter(Boolean)
})

const onDrag = (event) => {
  if (!isDragging) return
  const delta = event.clientX - startX
  const next = Math.min(maxTreeWidth, Math.max(minTreeWidth, startWidth + delta))
  treeWidth.value = next
}

const stopDrag = () => {
  if (!isDragging) return
  isDragging = false
  document.body.classList.remove('is-resizing')
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', stopDrag)
}

const startDrag = (event) => {
  isDragging = true
  startX = event.clientX
  startWidth = treeWidth.value
  document.body.classList.add('is-resizing')
  document.addEventListener('mousemove', onDrag)
  document.addEventListener('mouseup', stopDrag)
}

const onMapResize = (event) => {
  if (!isMapResizing) return
  const delta = event.clientY - mapResizeStartY
  const next = Math.min(maxMapHeight, Math.max(minMapHeight, mapResizeStartHeight + delta))
  mapHeight.value = next
}

const stopMapResize = () => {
  if (!isMapResizing) return
  isMapResizing = false
  document.body.classList.remove('is-resizing')
  document.removeEventListener('mousemove', onMapResize)
  document.removeEventListener('mouseup', stopMapResize)
}

const startMapResize = (event) => {
  if (!hasGeometry.value || !showMap.value) return
  isMapResizing = true
  mapResizeStartY = event.clientY
  mapResizeStartHeight = mapHeight.value
  document.body.classList.add('is-resizing')
  document.addEventListener('mousemove', onMapResize)
  document.addEventListener('mouseup', stopMapResize)
}

const loadTree = async () => {
  loadingTree.value = true
  try {
    const response = await dataExplorerAPI.getTree()
    treeData.value = (response.data.data || []).map((res) => {
      const schemas = (res.schemas || []).map((schema) => ({
        id: `schema-${res.id}-${schema.name}`,
        type: 'schema',
        label: schema.name,
        resourceId: res.id,
        schema: schema.name,
        children: (schema.tables || []).map((table) => ({
          id: `table-${res.id}-${schema.name}-${table.name}`,
          type: 'table',
          label: table.name,
          resourceId: res.id,
          schema: schema.name,
          table: table.name
        }))
      }))
      return {
        id: `resource-${res.id}`,
        type: 'resource',
        label: res.name,
        resourceId: res.id,
        children: schemas
      }
    })
  } catch (error) {
    ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingTree.value = false
  }
}

const loadPreview = async () => {
  if (!selectedTable.value) return
  loadingPreview.value = true
  try {
    const params = {
      resource_id: selectedTable.value.resourceId,
      schema: selectedTable.value.schema,
      table: selectedTable.value.table,
      page: currentPage.value,
      page_size: pageSize.value
    }
    const response = await dataExplorerAPI.getPreview(params)
    preview.columns = response.data.columns || []
    const baseKey = `${selectedTable.value.resourceId || 'res'}-${selectedTable.value.schema || 'schema'}-${selectedTable.value.table || 'table'}`
    preview.rows = (response.data.rows || []).map((row, index) => ({
      ...row,
      __rowKey: `${baseKey}-${(currentPage.value - 1) * pageSize.value + index}`
    }))
    preview.total = response.data.total || 0
    geometryColumns.value = response.data.geometry_columns || []
    currentRowKey.value = ''
    if (tableRef.value) {
      tableRef.value.setCurrentRow(null)
    }
  } catch (error) {
    ElMessage.error('加载数据预览失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingPreview.value = false
  }
}

const handleNodeClick = (nodeData) => {
  if (nodeData.type !== 'table') return
  selectedTable.value = nodeData
  currentPage.value = 1
  loadPreview()
}

const handlePageChange = (page) => {
  currentPage.value = page
  loadPreview()
}

const clearMapContainer = () => {
  if (mapContainer.value) {
    mapContainer.value.innerHTML = ''
  }
}

const destroyGaodeMap = () => {
  if (gaodeOverlays.length > 0) {
    gaodeOverlays.forEach((overlay) => {
      if (overlay?.setMap) {
        overlay.setMap(null)
      } else if (overlay?.destroy) {
        overlay.destroy()
      }
    })
    gaodeOverlays = []
  }
  if (gaodeEventsBound && gaodeMapInstance?.off) {
    gaodeMapInstance.off('moveend', updateGaodeViewState)
    gaodeMapInstance.off('zoomend', updateGaodeViewState)
    gaodeEventsBound = false
  }
  if (gaodeMapInstance?.destroy) {
    gaodeMapInstance.destroy()
  }
  gaodeMapInstance = null
  if (gaodeInfoWindow) {
    gaodeInfoWindow.close()
    gaodeInfoWindow = null
  }
  rowOverlayMap.clear()
}

const destroyTiandituMap = () => {
  if (olMap) {
    if (olMapClickKey) {
      unByKey(olMapClickKey)
      olMapClickKey = null
    }
    if (olViewEventKeys.length > 0) {
      olViewEventKeys.forEach((key) => unByKey(key))
      olViewEventKeys = []
    }
    if (olPopupOverlay) {
      olMap.removeOverlay(olPopupOverlay)
    }
    olMap.setTarget(null)
  }
  olMap = null
  olVectorLayer = null
  olVectorSource = null
  currentOlBaseType = ''
  olPopupOverlay = null
  olPopupElement = null
  rowFeatureMap.clear()
}

const hideOlPopup = () => {
  if (olPopupOverlay) {
    olPopupOverlay.setPosition(undefined)
  }
}

const destroyMap = () => {
  destroyGaodeMap()
  destroyTiandituMap()
  clearMapContainer()
  rowOverlayMap.clear()
  rowFeatureMap.clear()
  hideOlPopup()
  stopMapResize()
  currentRowKey.value = ''
}

const ensureGaodeMap = async () => {
  if (!mapConfig.value.amapKey) {
    ElMessage.warning('未配置高德地图 Key，无法加载高德底图')
    return null
  }

  if (mapConfig.value.amapSecurityJsCode && typeof window !== 'undefined') {
    window._AMapSecurityConfig = {
      ...(window._AMapSecurityConfig || {}),
      securityJsCode: mapConfig.value.amapSecurityJsCode
    }
  }

  if (!amapLib) {
    try {
      amapLib = await AMapLoader.load({
        key: mapConfig.value.amapKey,
        version: '2.0',
        plugins: ['AMap.Scale', 'AMap.ToolBar', 'AMap.CircleMarker']
      })
    } catch (error) {
      console.error('高德地图加载失败', error)
      ElMessage.error('高德底图加载失败，请检查网络或密钥配置')
      return null
    }
  }

  if (!mapContainer.value) return null

  const initialCenter = mapViewState?.center && isFinite(mapViewState.center[0]) && isFinite(mapViewState.center[1]) ? mapViewState.center : DEFAULT_CENTER
  const initialZoom = mapViewState && isFinite(mapViewState.zoom) ? mapViewState.zoom : 4

  if (!gaodeMapInstance) {
    clearMapContainer()
    gaodeMapInstance = new amapLib.Map(mapContainer.value, {
      viewMode: '2D',
      zoom: initialZoom,
      center: initialCenter,
      mapStyle: 'amap://styles/normal',
      pitch: 0,
      showLabel: true
    })

    if (amapLib.Scale) {
      gaodeMapInstance.addControl(new amapLib.Scale())
    }
    if (amapLib.ToolBar) {
      gaodeMapInstance.addControl(new amapLib.ToolBar())
    }
    gaodeInfoWindow = new amapLib.InfoWindow({ offset: new amapLib.Pixel(0, -20) })
  } else if (initialCenter && gaodeMapInstance.setZoomAndCenter) {
    gaodeMapInstance.setZoomAndCenter(initialZoom, initialCenter)
  }

  bindGaodeEvents()

  return {
    AMap: amapLib,
    map: gaodeMapInstance
  }
}

const updateGaodeOverlays = (AMap, map, features, { preserveView = false } = {}) => {
  if (!AMap || !map) return

  if (gaodeOverlays.length > 0) {
    gaodeOverlays.forEach((overlay) => {
      if (overlay?.setMap) {
        overlay.setMap(null)
      } else if (overlay?.destroy) {
        overlay.destroy()
      }
    })
    gaodeOverlays = []
  }
  rowOverlayMap.clear()
  if (gaodeInfoWindow) {
    gaodeInfoWindow.close()
  }

  const overlays = []

  const attachOverlayEvents = (overlay, rowData) => {
    if (!overlay) return
    const rowKey = rowData?.__rowKey
    if (rowKey) {
      const list = rowOverlayMap.get(rowKey) || []
      list.push(overlay)
      rowOverlayMap.set(rowKey, list)
    }
    overlay.on('click', (event) => {
      const coordinate = event?.lnglat || overlay.getPosition?.() || overlay.getBounds?.()?.getCenter?.()
      currentRowKey.value = rowKey || ''
      if (tableRef.value && rowData) {
        tableRef.value.setCurrentRow(rowData)
      }
      showGaodePopup(rowData, coordinate)
    })
  }

  const createMarker = (lng, lat, rowData) => {
    if (!isFinite(lng) || !isFinite(lat)) return null
    if (AMap.CircleMarker) {
      return new AMap.CircleMarker({
        center: [lng, lat],
        radius: 6,
        strokeColor: '#ffffff',
        strokeWeight: 2,
        fillColor: '#409EFF',
        fillOpacity: 0.9
      })
    }
    const div = document.createElement('div')
    div.className = 'gaode-point-marker'
    return new AMap.Marker({
      position: [lng, lat],
      offset: new AMap.Pixel(-6, -6),
      content: div
    })
  }

  const createPolygon = (rings) =>
    new AMap.Polygon({
      path: rings,
      strokeColor: '#67C23A',
      strokeWeight: 2,
      strokeOpacity: 0.8,
      fillColor: '#67C23A',
      fillOpacity: 0.25
    })

  const createPolyline = (path) =>
    new AMap.Polyline({
      path,
      strokeColor: '#409EFF',
      strokeWeight: 3,
      strokeOpacity: 0.9
    })

  features.forEach((feature) => {
    const geometry = feature?.geometry
    const rowData = feature?.properties || {}
    if (!geometry?.type || !geometry.coordinates) return

    switch (geometry.type) {
      case 'Point': {
        const marker = createMarker(geometry.coordinates[0], geometry.coordinates[1], rowData)
        if (marker) {
          overlays.push(marker)
          attachOverlayEvents(marker, rowData)
        }
        break
      }
      case 'MultiPoint': {
        geometry.coordinates.forEach((coord) => {
          const marker = createMarker(coord[0], coord[1], rowData)
          if (marker) {
            overlays.push(marker)
            attachOverlayEvents(marker, rowData)
          }
        })
        break
      }
      case 'LineString': {
        const path = geometry.coordinates.map(([lng, lat]) => [lng, lat])
        const polyline = createPolyline(path)
        overlays.push(polyline)
        attachOverlayEvents(polyline, rowData)
        break
      }
      case 'MultiLineString': {
        geometry.coordinates.forEach((line) => {
          const path = line.map(([lng, lat]) => [lng, lat])
          const polyline = createPolyline(path)
          overlays.push(polyline)
          attachOverlayEvents(polyline, rowData)
        })
        break
      }
      case 'Polygon': {
        const rings = geometry.coordinates.map((ring) => ring.map(([lng, lat]) => [lng, lat]))
        const polygon = createPolygon(rings)
        overlays.push(polygon)
        attachOverlayEvents(polygon, rowData)
        break
      }
      case 'MultiPolygon': {
        geometry.coordinates.forEach((polygonCoords) => {
          const rings = polygonCoords.map((ring) => ring.map(([lng, lat]) => [lng, lat]))
          const polygon = createPolygon(rings)
          overlays.push(polygon)
          attachOverlayEvents(polygon, rowData)
        })
        break
      }
      default:
        break
    }
  })

  if (overlays.length === 0) {
    if (!preserveView) {
      map.setZoomAndCenter(4, DEFAULT_CENTER)
      mapViewState = { center: DEFAULT_CENTER, zoom: 4 }
    } else {
      updateGaodeViewState()
    }
    return
  }

  map.add(overlays)
  gaodeOverlays = overlays
  if (!preserveView) {
    map.setFitView(overlays, false, [20, 20, 20, 20])
    setTimeout(updateGaodeViewState, 0)
  } else {
    updateGaodeViewState()
  }
}

const renderGaodeMap = async () => {
  const context = await ensureGaodeMap()
  if (!context) return
  updateGaodeOverlays(context.AMap, context.map, geoFeatures.value)
}
const tiandituPointStyle = new Style({
  image: new CircleStyle({
    radius: 6,
    fill: new Fill({ color: '#409EFF' }),
    stroke: new Stroke({ color: '#ffffff', width: 2 })
  })
})

const tiandituPolygonStyle = new Style({
  stroke: new Stroke({ color: '#67C23A', width: 2 }),
  fill: new Fill({ color: 'rgba(103, 194, 58, 0.25)' })
})

const geoJSONFormat = new GeoJSON()

const createTiandituLayers = (baseType, key) => {
  const isImage = baseType === 'tiandituImage'
  const baseId = isImage ? 'img' : 'vec'
  const labelId = isImage ? 'cia' : 'cva'

  const createLayer = (layerId) =>
    new TileLayer({
      source: new XYZ({
        url: `https://t{0-7}.tianditu.gov.cn/${layerId}_w/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=${layerId}&STYLE=default&TILEMATRIXSET=w&FORMAT=tiles&TILEMATRIX={z}&TILEROW={y}&TILECOL={x}&tk=${key}`,
        maxZoom: 18,
        crossOrigin: 'anonymous'
      })
    })

  const baseLayer = createLayer(baseId)
  const labelLayer = createLayer(labelId)
  labelLayer.setZIndex(100)
  return { baseLayer, labelLayer }
}

const ensureTiandituMap = (baseType) => {
  const tdtKey = mapConfig.value.tdtKey || DEFAULT_TDT_KEY
  if (!tdtKey) {
    ElMessage.warning('未配置天地图 Key，无法加载天地图底图')
    return null
  }

  const container = mapContainer.value
  if (!container) return null

  const initialCenter = mapViewState?.center && isFinite(mapViewState.center[0]) && isFinite(mapViewState.center[1]) ? mapViewState.center : DEFAULT_CENTER
  const initialZoom = mapViewState && isFinite(mapViewState.zoom) ? mapViewState.zoom : 4

  if (!olMap) {
    olVectorSource = new VectorSource()
    olVectorLayer = new VectorLayer({
      source: olVectorSource,
      style: (feature) => {
        const type = feature.getGeometry()?.getType()
        if (type === 'Point' || type === 'MultiPoint') {
          return tiandituPointStyle
        }
        return tiandituPolygonStyle
      }
    })

    olMap = new OlMap({
      target: container,
      layers: [],
      view: new OlView({
        center: fromLonLat(initialCenter),
        zoom: initialZoom,
        maxZoom: 18,
        minZoom: 3
      })
    })
    olPopupElement = document.createElement('div')
    olPopupElement.className = 'map-popup'
    olPopupOverlay = new Overlay({
      element: olPopupElement,
      offset: [0, -12],
      positioning: 'bottom-center',
      stopEvent: true
    })
    olMap.addOverlay(olPopupOverlay)
    olMapClickKey = olMap.on('singleclick', handleOlMapSingleClick)
  } else if (olMap.getTarget() !== container) {
    olMap.setTarget(container)
  }

  if (olPopupOverlay && !olMap.getOverlays().getArray().includes(olPopupOverlay)) {
    olMap.addOverlay(olPopupOverlay)
  }
  if (!olMapClickKey) {
    olMapClickKey = olMap.on('singleclick', handleOlMapSingleClick)
  }

  if (currentOlBaseType !== baseType) {
    const { baseLayer, labelLayer } = createTiandituLayers(baseType, tdtKey)
    const layers = olMap.getLayers()
    layers.clear()
    if (baseLayer) layers.push(baseLayer)
    if (labelLayer) layers.push(labelLayer)
    if (olVectorLayer) layers.push(olVectorLayer)
    currentOlBaseType = baseType
  } else {
    const layers = olMap.getLayers()
    if (olVectorLayer && !layers.getArray().includes(olVectorLayer)) {
      layers.push(olVectorLayer)
    }
  }

  bindOlEvents()
  if (!activeMapType) {
    updateOlViewState()
  } else if (initialCenter) {
    applyOlViewState()
  }

  return olMap
}

const updateTiandituOverlays = (map, features, { preserveView = false } = {}) => {
  if (!map || !olVectorSource) return

  hideOlPopup()
  rowFeatureMap.clear()
  olVectorSource.clear()

  if (!features || features.length === 0) {
    if (!preserveView) {
      map.getView().setCenter(fromLonLat(DEFAULT_CENTER))
      map.getView().setZoom(4)
      mapViewState = { center: DEFAULT_CENTER, zoom: 4 }
    } else {
      updateOlViewState()
    }
    return
  }

  const featureCollection = {
    type: 'FeatureCollection',
    features
  }

  const olFeatures = geoJSONFormat.readFeatures(featureCollection, {
    dataProjection: 'EPSG:4326',
    featureProjection: 'EPSG:3857'
  })

  if (olFeatures.length === 0) {
    map.getView().setCenter(fromLonLat(DEFAULT_CENTER))
    map.getView().setZoom(4)
    return
  }

  olFeatures.forEach((feature, index) => {
    const rowData = features[index]?.properties || {}
    feature.set('rowData', rowData)
    const rowKey = rowData?.__rowKey
    feature.set('rowKey', rowKey)
    if (rowKey) {
      rowFeatureMap.set(rowKey, feature)
    }
  })

  olVectorSource.addFeatures(olFeatures)

  const extent = olVectorSource.getExtent()
  if (extent && isFinite(extent[0])) {
    if (!preserveView) {
      map.getView().fit(extent, {
        padding: [20, 20, 20, 20],
        maxZoom: 14,
        duration: 300
      })
      setTimeout(updateOlViewState, 0)
    } else {
      updateOlViewState()
    }
  } else if (preserveView) {
    updateOlViewState()
  }
}

const displayColumns = computed(() => {
  if (!preview.columns || preview.columns.length === 0) return []
  const geometrySet = new Set(geometryColumns.value || [])
  const filtered = preview.columns.filter((col) => !geometrySet.has(col))
  return filtered.length > 0 ? filtered : preview.columns
})

const escapeHtml = (value) =>
  String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')

const formatCellValue = (value) => {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch (error) {
      return '[object]'
    }
  }
  return String(value)
}

const buildInfoContent = (row) => {
  if (!row) {
    return '<div class="map-popup-content">暂无数据</div>'
  }
  const columns = displayColumns.value
  const rowsHtml = columns
    .map((col) => {
      const value = formatCellValue(row[col])
      return `<div class="map-popup-row"><span class="map-popup-label">${escapeHtml(col)}</span><span class="map-popup-value">${escapeHtml(value)}</span></div>`
    })
    .join('')
  return `<div class="map-popup-content">${rowsHtml || '<div class="map-popup-row">暂无可展示字段</div>'}</div>`
}

const showGaodePopup = (row, lnglat) => {
  if (!row || !gaodeMapInstance) return
  const AMapModule = amapLib || window.AMap
  if (!AMapModule) return
  if (!gaodeInfoWindow) {
    gaodeInfoWindow = new AMapModule.InfoWindow({
      offset: new AMapModule.Pixel(0, -20)
    })
  }
  gaodeInfoWindow.setContent(`<div class="map-popup">${buildInfoContent(row)}</div>`)
  let position = lnglat
  if (Array.isArray(position)) {
    position = new AMapModule.LngLat(position[0], position[1])
  }
  if (!position) {
    const geometry = getRowGeometry(row)
    const center = getGeometryCenter(geometry)
    if (center) {
      position = new AMapModule.LngLat(center[0], center[1])
    }
  }
  if (position) {
    gaodeInfoWindow.open(gaodeMapInstance, position)
  }
}

const showOlPopup = (row, coordinate) => {
  if (!row || !olMap) return
  if (!olPopupOverlay || !olPopupElement) {
    olPopupElement = document.createElement('div')
    olPopupElement.className = 'map-popup'
    olPopupOverlay = new Overlay({
      element: olPopupElement,
      offset: [0, -12],
      positioning: 'bottom-center',
      stopEvent: true
    })
    olMap.addOverlay(olPopupOverlay)
  }
  olPopupElement.innerHTML = buildInfoContent(row)
  if (coordinate) {
    olPopupOverlay.setPosition(coordinate)
  }
}

function handleOlMapSingleClick(evt) {
  if (!olMap) return
  const feature = olMap.forEachFeatureAtPixel(evt.pixel, (feature) => feature)
  if (feature) {
    const rowData = feature.get('rowData')
    if (rowData) {
      currentRowKey.value = rowData.__rowKey || ''
      if (tableRef.value) {
        tableRef.value.setCurrentRow(rowData)
      }
      const geometry = feature.getGeometry()
      let coordinate = evt.coordinate
      if (geometry) {
        const type = geometry.getType()
        if (type === 'Point') {
          coordinate = geometry.getCoordinates()
        } else if (type === 'MultiPoint') {
          coordinate = geometry.getClosestPoint(evt.coordinate)
        } else if (type.includes('Polygon') && geometry.getInteriorPoint) {
          coordinate = geometry.getInteriorPoint().getCoordinates()
        } else {
          coordinate = geometry.getClosestPoint(evt.coordinate)
        }
      }
      showOlPopup(rowData, coordinate)
    }
  } else {
    hideOlPopup()
  }
}

const getRowGeometry = (row) => {
  if (!row || !activeGeometryColumn.value) return null
  const column = activeGeometryColumn.value
  const value = row[column]
  if (!value) return null
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch (error) {
      return null
    }
  }
  if (typeof value === 'object') {
    return value
  }
  return null
}

const getGeometryBounds = (geometry) => {
  if (!geometry?.coordinates) return null
  let minLng = Infinity
  let minLat = Infinity
  let maxLng = -Infinity
  let maxLat = -Infinity

  const traverse = (coords) => {
    if (typeof coords[0] === 'number') {
      const [lng, lat] = coords
      minLng = Math.min(minLng, lng)
      maxLng = Math.max(maxLng, lng)
      minLat = Math.min(minLat, lat)
      maxLat = Math.max(maxLat, lat)
      return
    }
    coords.forEach(traverse)
  }

  traverse(geometry.coordinates)

  if (!isFinite(minLng) || !isFinite(minLat) || !isFinite(maxLng) || !isFinite(maxLat)) {
    return null
  }

  return [
    [minLng, minLat],
    [maxLng, maxLat]
  ]
}

const getGeometryCenter = (geometry) => {
  if (!geometry) return null
  if (geometry.type === 'Point') {
    return geometry.coordinates
  }
  if (geometry.type === 'MultiPoint') {
    const points = geometry.coordinates
    if (!points.length) return null
    const sum = points.reduce(
      (acc, coord) => {
        acc[0] += coord[0]
        acc[1] += coord[1]
        return acc
      },
      [0, 0]
    )
    return [sum[0] / points.length, sum[1] / points.length]
  }
  const bounds = getGeometryBounds(geometry)
  if (!bounds) return null
  return [
    (bounds[0][0] + bounds[1][0]) / 2,
    (bounds[0][1] + bounds[1][1]) / 2
  ]
}

const focusRowOnMap = (row, options = { openPopup: false }) => {
  if (!row) return
  const rowKey = row.__rowKey
  if (baseMapType.value === GAODE_BASE_MAP_VALUE && gaodeMapInstance) {
    const overlays = rowOverlayMap.get(rowKey)
    if (overlays && overlays.length > 0) {
      if (overlays.length === 1 && overlays[0].getPosition) {
        const position = overlays[0].getPosition()
        if (position) {
          gaodeMapInstance.setZoomAndCenter(Math.max(gaodeMapInstance.getZoom(), 8), position)
          if (options.openPopup) {
            showGaodePopup(row, position)
          }
          setTimeout(updateGaodeViewState, 0)
        }
      } else if (gaodeMapInstance.setFitView) {
        gaodeMapInstance.setFitView(overlays, false, [20, 20, 20, 20])
        if (options.openPopup) {
          const overlay = overlays[0]
          const position = overlay?.getPosition?.() || overlay?.getBounds?.()?.getCenter?.()
          showGaodePopup(row, position)
        }
        setTimeout(updateGaodeViewState, 0)
      }
      return
    }
    const geometry = getRowGeometry(row)
    const center = getGeometryCenter(geometry)
    const AMapModule = amapLib || window.AMap
    if (center && AMapModule) {
      gaodeMapInstance.setZoomAndCenter(Math.max(gaodeMapInstance.getZoom(), 8), new AMapModule.LngLat(center[0], center[1]))
      if (options.openPopup) {
        showGaodePopup(row, center)
      }
      setTimeout(updateGaodeViewState, 0)
    }
    return
  }

  if ((baseMapType.value === 'tiandituVector' || baseMapType.value === 'tiandituImage') && olMap) {
    const feature = rowFeatureMap.get(rowKey)
    if (feature) {
      const geometry = feature.getGeometry()
      if (geometry) {
        const extent = geometry.getExtent()
        if (extent && isFinite(extent[0])) {
          olMap.getView().fit(extent, {
            padding: [20, 20, 20, 20],
            maxZoom: 16,
            duration: 300
          })
          setTimeout(updateOlViewState, 0)
        }
        if (options.openPopup) {
          const type = geometry.getType()
          let coordinate
          if (type === 'Point') {
            coordinate = geometry.getCoordinates()
          } else if (type === 'MultiPoint') {
            coordinate = geometry.getClosestPoint(olMap.getView().getCenter())
          } else if (type.includes('Polygon') && geometry.getInteriorPoint) {
            coordinate = geometry.getInteriorPoint().getCoordinates()
          } else {
            coordinate = geometry.getClosestPoint(olMap.getView().getCenter())
          }
          showOlPopup(row, coordinate)
        }
      }
      return
    }
    const geometry = getRowGeometry(row)
    const center = getGeometryCenter(geometry)
    if (center) {
      const coordinate = fromLonLat(center)
      olMap.getView().animate({ center: coordinate, duration: 300, zoom: Math.max(olMap.getView().getZoom(), 8) })
      if (options.openPopup) {
        showOlPopup(row, coordinate)
      }
      setTimeout(updateOlViewState, 300)
    }
  }
}

const handleRowClick = (row) => {
  currentRowKey.value = row?.__rowKey || ''
  if (tableRef.value) {
    tableRef.value.setCurrentRow(row)
  }
  if (gaodeInfoWindow) {
    gaodeInfoWindow.close()
  }
  hideOlPopup()
  focusRowOnMap(row, { openPopup: false })
}

const getRowKey = (row) =>
  row?.__rowKey || row?.id || row?.ID || row?._id || row?.uuid || row?.code || row?.name || String(preview.rows.indexOf(row))

const getFeatureBounds = (features) => {
  let minLng = Infinity
  let minLat = Infinity
  let maxLng = -Infinity
  let maxLat = -Infinity

  const processCoords = (coords) => {
    if (typeof coords[0] === 'number') {
      const [lng, lat] = coords
      minLng = Math.min(minLng, lng)
      maxLng = Math.max(maxLng, lng)
      minLat = Math.min(minLat, lat)
      maxLat = Math.max(maxLat, lat)
      return
    }
    coords.forEach(processCoords)
  }

  features.forEach((feature) => {
    if (!feature?.geometry?.coordinates) return
    processCoords(feature.geometry.coordinates)
  })

  if (!isFinite(minLng) || !isFinite(minLat) || !isFinite(maxLng) || !isFinite(maxLat)) {
    return null
  }
  return [
    [minLng, minLat],
    [maxLng, maxLat]
  ]
}

const renderMap = async () => {
  if (!hasGeometry.value || !showMap.value) {
    captureViewState()
    destroyMap()
    activeMapType = ''
    return
  }

  await nextTick()

  if (!mapContainer.value) return

  if (!baseMapType.value && baseMapOptions.value.length > 0) {
    baseMapType.value = baseMapOptions.value[0].value
    return
  }

  const currentType = baseMapType.value
  const switchingBaseMap = activeMapType && activeMapType !== currentType

  if (activeMapType) {
    captureViewState()
  }

  if (currentType === GAODE_BASE_MAP_VALUE) {
    if (activeMapType !== GAODE_BASE_MAP_VALUE) {
      destroyTiandituMap()
    }
    const context = await ensureGaodeMap()
    if (!context) {
      return
    }
    applyGaodeViewState()
    updateGaodeOverlays(context.AMap, context.map, geoFeatures.value, { preserveView: switchingBaseMap })
    activeMapType = GAODE_BASE_MAP_VALUE
    return
  }

  if (currentType === 'tiandituVector' || currentType === 'tiandituImage') {
    if (activeMapType !== currentType) {
      destroyGaodeMap()
    }
    const map = await ensureTiandituMap(currentType)
    if (!map) {
      return
    }
    applyOlViewState()
    updateTiandituOverlays(map, geoFeatures.value, { preserveView: switchingBaseMap })
    activeMapType = currentType
    return
  }

  // 未知底图类型时重置
  destroyMap()
  activeMapType = ''
}

watch([showMap, hasGeometry, geoFeatures], () => {
  renderMap()
})

watch(baseMapType, () => {
  renderMap()
})

onMounted(() => {
  loadMapConfig()
  loadTree()
})

onBeforeUnmount(() => {
  stopDrag()
  destroyMap()
})
</script>

<style scoped>
.data-explorer {
  padding: 10px;
}

.split-container {
  display: grid;
  grid-template-columns: 320px 8px 1fr;
  min-height: 620px;
  align-items: stretch;
  width: 100%;
}

.tree-panel {
  max-height: 636px;
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.tree-panel :deep(.el-card),
.preview-wrapper :deep(.el-card) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.tree-panel :deep(.el-card__body) {
  flex: 1;
  overflow: auto;
}

.preview-wrapper :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.map-controls {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toggle-wrapper {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.base-map-select {
  min-width: 160px;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tree-node .label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.splitter {
  cursor: col-resize;
  position: relative;
  width: 8px;
  height: 100%;
}

.splitter::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 2px;
  border-radius: 1px;
  background: var(--el-color-primary-light-9);
}

.splitter:hover::after,
body.is-resizing .splitter::after {
  background: var(--el-color-primary);
}

.preview-wrapper {
  min-width: 320px;
  overflow: hidden;
  display: flex;
  height: 100%;
}

.preview-panel {
  flex: 1;
  min-height: 600px;
  display: flex;
  flex-direction: column;
}

.preview-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.map-container {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: hidden;
  position: relative;
  z-index: 1;
  margin-bottom: 0;
  min-height: 140px;
}

.map-table-splitter {
  height: 8px;
  cursor: row-resize;
  position: relative;
  margin: -4px 0 4px;
}

.map-table-splitter::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 2px;
  background: var(--el-color-primary-light-8);
  border-radius: 2px;
}

.map-table-splitter:hover::after,
body.is-resizing .map-table-splitter::after {
  background: var(--el-color-primary);
}

.table-wrapper {
  flex: 1 1 auto;
  min-height: 220px;
  display: flex;
  flex-direction: column;
}

.table-wrapper :deep(.el-table) {
  flex: 1;
}

:deep(.map-popup) {
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
  padding: 8px 12px;
  max-width: 280px;
  font-size: 12px;
  color: var(--el-text-color-primary);
}

:deep(.map-popup-content) {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

:deep(.map-popup-row) {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  line-height: 1.4;
}

:deep(.map-popup-label) {
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

:deep(.map-popup-value) {
  flex: 1;
  text-align: right;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.gaode-point-marker {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background-color: #409EFF;
  border: 2px solid #ffffff;
  box-shadow: 0 0 6px rgba(64, 158, 255, 0.4);
}

.empty-state {
  height: 520px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 12px;
}

.pagination .tip {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
