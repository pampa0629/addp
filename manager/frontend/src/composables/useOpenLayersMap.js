import { ref, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
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

const DEFAULT_CENTER = [104.0668, 30.5728]
const DEFAULT_TDT_KEY = import.meta.env.VITE_TDT_KEY || ''

const pointStyle = new Style({
  image: new CircleStyle({
    radius: 6,
    fill: new Fill({ color: '#409EFF' }),
    stroke: new Stroke({ color: '#ffffff', width: 2 })
  })
})

const polygonStyle = new Style({
  stroke: new Stroke({ color: '#67C23A', width: 2 }),
  fill: new Fill({ color: 'rgba(103, 194, 58, 0.25)' })
})

const geoJSONFormat = new GeoJSON()

/**
 * OpenLayers 地图管理 Composable
 * @param {Object} config - 地图配置 { tdtKey }
 */
export function useOpenLayersMap(config) {
  const mapInstance = ref(null)
  const vectorSource = ref(null)
  const vectorLayer = ref(null)
  const popupOverlay = ref(null)
  const popupElement = ref(null)
  const featureStore = new Map()

  let currentBaseType = ''
  let viewEventKeys = []
  let mapClickKey = null
  let viewState = { center: DEFAULT_CENTER, zoom: 4 }

  const updateViewState = () => {
    if (!mapInstance.value) return
    const view = mapInstance.value.getView?.()
    if (!view) return
    const center = view.getCenter?.()
    const zoom = view.getZoom?.()
    if (center && isFinite(zoom)) {
      const lonLat = toLonLat(center)
      if (lonLat && isFinite(lonLat[0]) && isFinite(lonLat[1])) {
        viewState = {
          center: lonLat,
          zoom
        }
      }
    }
  }

  const bindEvents = () => {
    if (!mapInstance.value) return
    const view = mapInstance.value.getView?.()
    if (!view) return
    if (viewEventKeys.length === 0) {
      viewEventKeys.push(view.on('change:center', updateViewState))
      viewEventKeys.push(view.on('change:resolution', updateViewState))
    }
  }

  const applyViewState = () => {
    if (!mapInstance.value || !viewState?.center) return
    const view = mapInstance.value.getView?.()
    if (!view) return
    const [lng, lat] = viewState.center
    if (!isFinite(lng) || !isFinite(lat)) return
    const zoom = isFinite(viewState.zoom) ? viewState.zoom : 4
    view.setCenter(fromLonLat([lng, lat]))
    view.setZoom(zoom)
  }

  const createBaseLayers = (baseType, key) => {
    if (!['tiandituVector', 'tiandituImage'].includes(baseType)) {
      return { baseLayer: null, labelLayer: null }
    }

    const isImage = baseType === 'tiandituImage'
    const baseId = isImage ? 'img' : 'vec'
    const labelId = isImage ? 'cia' : 'cva'

    const createLayer = (layerId) =>
      new TileLayer({
        source: new XYZ({
          url: `https://t{0-7}.tianditu.gov.cn/${layerId}_w/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=${layerId}&STYLE=default&TILEMATRIXSET=w&FORMAT=tiles&TILEMATRIX={z}&TILEROW={y}&TILECOL={x}&tk=${key}`,
          maxZoom: 18
          // 移除 crossOrigin: 'anonymous' 避免天地图 CORS 问题
        })
      })

    const baseLayer = createLayer(baseId)
    const labelLayer = createLayer(labelId)
    baseLayer.setZIndex(0)
    labelLayer.setZIndex(100)
    return { baseLayer, labelLayer }
  }

  const initMap = (container, baseType) => {
    if (!['tiandituVector', 'tiandituImage'].includes(baseType)) {
      return null
    }

    const tdtKey = config.tdtKey || DEFAULT_TDT_KEY
    if (!tdtKey) {
      ElMessage.warning('未配置天地图 Key，无法加载天地图底图')
      return null
    }

    if (!container) return null

    const initialCenter = viewState?.center && isFinite(viewState.center[0]) && isFinite(viewState.center[1])
      ? viewState.center
      : DEFAULT_CENTER
    const initialZoom = viewState && isFinite(viewState.zoom) ? viewState.zoom : 4

    if (!mapInstance.value) {
      vectorSource.value = new VectorSource()
      vectorLayer.value = new VectorLayer({
        source: vectorSource.value,
        style: (feature) => {
          const type = feature.getGeometry()?.getType()
          if (type === 'Point' || type === 'MultiPoint') {
            return pointStyle
          }
          return polygonStyle
        }
      })

      mapInstance.value = new OlMap({
        target: container,
        layers: [],
        view: new OlView({
          center: fromLonLat(initialCenter),
          zoom: initialZoom,
          maxZoom: 18,
          minZoom: 3
        })
      })

      popupElement.value = document.createElement('div')
      popupElement.value.className = 'map-popup'
      popupOverlay.value = new Overlay({
        element: popupElement.value,
        offset: [0, -12],
        positioning: 'bottom-center',
        stopEvent: true
      })
      mapInstance.value.addOverlay(popupOverlay.value)
    } else if (mapInstance.value.getTarget() !== container) {
      mapInstance.value.setTarget(container)
    }

    // 确保 popup overlay 存在
    if (popupOverlay.value && !mapInstance.value.getOverlays().getArray().includes(popupOverlay.value)) {
      mapInstance.value.addOverlay(popupOverlay.value)
    }

    // 更新底图图层
    if (currentBaseType !== baseType) {
      const { baseLayer, labelLayer } = createBaseLayers(baseType, tdtKey)
      const layers = mapInstance.value.getLayers()
      layers.clear()
      if (baseLayer) layers.push(baseLayer)
      if (labelLayer) layers.push(labelLayer)
      if (vectorLayer.value) layers.push(vectorLayer.value)
      currentBaseType = baseType
    } else {
      const layers = mapInstance.value.getLayers()
      if (vectorLayer.value && !layers.getArray().includes(vectorLayer.value)) {
        layers.push(vectorLayer.value)
      }
    }

    bindEvents()
    applyViewState()

    return mapInstance.value
  }

  const renderFeatures = (features, options = {}) => {
    if (!mapInstance.value || !vectorSource.value) return

    hidePopup()
    vectorSource.value.clear()
    featureStore.clear()

    if (!features || features.length === 0) {
      if (!options.preserveView) {
        mapInstance.value.getView().setCenter(fromLonLat(DEFAULT_CENTER))
        mapInstance.value.getView().setZoom(4)
        viewState = { center: DEFAULT_CENTER, zoom: 4 }
      } else {
        updateViewState()
      }
      return
    }

    // 调试：查看原始 features
    console.log('[OpenLayers] 收到 features:', features.length, '个')
    if (features.length > 0) {
      console.log('[OpenLayers] 第一个 feature 示例:', features[0])
    }

    const featureCollection = {
      type: 'FeatureCollection',
      features
    }

    let olFeatures
    try {
      olFeatures = geoJSONFormat.readFeatures(featureCollection, {
        dataProjection: 'EPSG:4326',
        featureProjection: 'EPSG:3857'
      })
      console.log('[OpenLayers] 解析后 olFeatures:', olFeatures.length, '个')
    } catch (error) {
      console.error('解析 GeoJSON 失败:', error)
      ElMessage.error('地图数据解析失败')
      return
    }

    if (olFeatures.length === 0) {
      mapInstance.value.getView().setCenter(fromLonLat(DEFAULT_CENTER))
      mapInstance.value.getView().setZoom(4)
      return
    }

    // 验证并添加有效的 feature
    const validFeatures = []
    olFeatures.forEach((feature, index) => {
      const geometry = feature.getGeometry()

      // 详细调试信息
      console.log(`[OpenLayers] Feature ${index}:`, {
        feature,
        geometry,
        geometryType: geometry?.getType?.(),
        hasExtent: typeof geometry?.getExtent === 'function',
        extentValue: geometry?.getExtent?.()
      })

      if (!geometry || typeof geometry.getExtent !== 'function') {
        console.warn('跳过无效几何对象 [索引:', index, ']:', feature, 'geometry:', geometry)
        return
      }

      const originalFeature = features[index]
      feature.set('originalFeature', originalFeature)
      const rowData = originalFeature?.properties || {}
      feature.set('rowData', rowData)
      const rowKey =
        rowData.__rowKey ||
        rowData.id ||
        rowData.ID ||
        rowData.uuid ||
        rowData.code ||
        rowData.name ||
        originalFeature?.id
      if (rowKey) {
        featureStore.set(rowKey, {
          feature,
          row: rowData,
          original: originalFeature
        })
      }
      validFeatures.push(feature)
    })

    console.log('[OpenLayers] 有效 features:', validFeatures.length, '个')

    if (validFeatures.length === 0) {
      console.warn('没有有效的几何对象')
      return
    }

    vectorSource.value.addFeatures(validFeatures)

    const extent = vectorSource.value.getExtent()
    if (extent && isFinite(extent[0])) {
      if (!options.preserveView) {
        mapInstance.value.getView().fit(extent, {
          padding: [20, 20, 20, 20],
          maxZoom: 14,
          duration: 300
        })
        setTimeout(updateViewState, 0)
      } else {
        updateViewState()
      }
    } else if (options.preserveView) {
      updateViewState()
    }

    // 绑定点击事件
    if (options.onFeatureClick && !mapClickKey) {
      mapClickKey = mapInstance.value.on('singleclick', (evt) => {
        const feature = mapInstance.value.forEachFeatureAtPixel(evt.pixel, (f) => f)
        if (feature) {
          const originalFeature = feature.get('originalFeature')
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
          options.onFeatureClick(originalFeature, coordinate)
        } else {
          hidePopup()
        }
      })
    }
  }

  const focusFeature = (rowKey, options = {}) => {
    if (!mapInstance.value || !rowKey) return false
    const record = featureStore.get(rowKey)
    if (!record) return false
    const { feature, row } = record
    const geometry = feature.getGeometry()
    if (!geometry || typeof geometry.getExtent !== 'function') {
      console.warn('跳过无效几何对象,无法聚焦')
      return false
    }

    const shouldFit = options.fit !== false
    const padding = options.padding || [40, 40, 40, 40]
    const maxZoom = options.maxZoom || 16

    if (shouldFit) {
      try {
        const extent = geometry.getExtent()
        if (extent && isFinite(extent[0])) {
          mapInstance.value.getView().fit(extent, {
            padding,
            maxZoom,
            duration: 300
          })
        }
      } catch (error) {
        console.error('获取几何范围失败:', error)
      }
    }

    if (options.openPopup && options.popupContent) {
      const content = typeof options.popupContent === 'function' ? options.popupContent(row) : options.popupContent
      if (content) {
        let coordinate = options.coordinate
        if (!coordinate) {
          try {
            const type = geometry.getType()
            if (type === 'Point') {
              coordinate = geometry.getCoordinates()
            } else if (type === 'MultiPoint') {
              coordinate = geometry.getClosestPoint(mapInstance.value.getView().getCenter())
            } else if (type.includes('Polygon') && geometry.getInteriorPoint) {
              coordinate = geometry.getInteriorPoint().getCoordinates()
            } else {
              coordinate = geometry.getClosestPoint(mapInstance.value.getView().getCenter())
            }
          } catch (error) {
            console.error('获取几何坐标失败:', error)
          }
        }
        if (coordinate) {
          showPopup(content, coordinate)
        }
      }
    } else if (!options.keepPopup) {
      hidePopup()
    }

    setTimeout(updateViewState, 0)
    return true
  }

  const showPopup = (content, coordinate) => {
    if (!popupOverlay.value || !popupElement.value) return
    popupElement.value.innerHTML = content
    if (coordinate) {
      popupOverlay.value.setPosition(coordinate)
    }
  }

  const hidePopup = () => {
    if (popupOverlay.value) {
      popupOverlay.value.setPosition(undefined)
    }
  }

  const destroy = () => {
    if (mapInstance.value) {
      if (mapClickKey) {
        unByKey(mapClickKey)
        mapClickKey = null
      }
      if (viewEventKeys.length > 0) {
        viewEventKeys.forEach((key) => unByKey(key))
        viewEventKeys = []
      }
      if (popupOverlay.value) {
        mapInstance.value.removeOverlay(popupOverlay.value)
      }
      mapInstance.value.setTarget(null)
    }
    mapInstance.value = null
    vectorLayer.value = null
    vectorSource.value = null
    currentBaseType = ''
    popupOverlay.value = null
    popupElement.value = null
    featureStore.clear()
  }

  onBeforeUnmount(() => {
    destroy()
  })

  return {
    mapInstance,
    vectorSource,
    vectorLayer,
    initMap,
    renderFeatures,
    focusFeature,
    showPopup,
    hidePopup,
    updateViewState,
    applyViewState,
    destroy
  }
}
