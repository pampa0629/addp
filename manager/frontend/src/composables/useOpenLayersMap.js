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
          maxZoom: 18,
          crossOrigin: 'anonymous'
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

    const featureCollection = {
      type: 'FeatureCollection',
      features
    }

    const olFeatures = geoJSONFormat.readFeatures(featureCollection, {
      dataProjection: 'EPSG:4326',
      featureProjection: 'EPSG:3857'
    })

    if (olFeatures.length === 0) {
      mapInstance.value.getView().setCenter(fromLonLat(DEFAULT_CENTER))
      mapInstance.value.getView().setZoom(4)
      return
    }

    olFeatures.forEach((feature, index) => {
      const originalFeature = features[index]
      feature.set('originalFeature', originalFeature)
    })

    vectorSource.value.addFeatures(olFeatures)

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
    showPopup,
    hidePopup,
    updateViewState,
    applyViewState,
    destroy
  }
}
