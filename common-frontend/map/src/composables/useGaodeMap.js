import { ref, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import AMapLoader from '@amap/amap-jsapi-loader'
import { mapDisplayCoordinate, mapSourceCoordinate } from '../utils/gcj02'

const DEFAULT_CENTER = [104.0668, 30.5728]
const POINT_STYLE = {
  radius: 6,
  strokeColor: '#ffffff',
  strokeWeight: 2,
  fillColor: '#409EFF',
  fillOpacity: 0.9,
  zIndex: 20
}
const POINT_HIGHLIGHT_STYLE = {
  radius: 10,
  strokeColor: '#FFD700',
  strokeWeight: 3,
  fillColor: '#FFD700',
  fillOpacity: 0.95,
  zIndex: 120
}
const LINE_STYLE = {
  strokeColor: '#409EFF',
  strokeWeight: 3,
  strokeOpacity: 0.9,
  zIndex: 20
}
const LINE_HIGHLIGHT_STYLE = {
  strokeColor: '#FFD700',
  strokeWeight: 6,
  strokeOpacity: 1,
  zIndex: 120
}
const POLYGON_STYLE = {
  strokeColor: '#67C23A',
  strokeWeight: 2,
  strokeOpacity: 0.8,
  fillColor: '#67C23A',
  fillOpacity: 0.25,
  zIndex: 20
}
const POLYGON_HIGHLIGHT_STYLE = {
  strokeColor: '#FFD700',
  strokeWeight: 4,
  strokeOpacity: 1,
  fillColor: '#FFD700',
  fillOpacity: 0.32,
  zIndex: 120
}

const getGeometryBounds = (geometry) => {
  if (!geometry?.coordinates) return null
  let minLng = Infinity
  let minLat = Infinity
  let maxLng = -Infinity
  let maxLat = -Infinity

  const traverse = (coords) => {
    if (!coords) return
    if (typeof coords[0] === 'number') {
      const [lng, lat] = coords
      if (!isFinite(lng) || !isFinite(lat)) return
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
  return {
    minLng,
    minLat,
    maxLng,
    maxLat
  }
}

const getGeometryCenter = (geometry) => {
  const bounds = getGeometryBounds(geometry)
  if (!bounds) return null
  return [(bounds.minLng + bounds.maxLng) / 2, (bounds.minLat + bounds.maxLat) / 2]
}

const positionToArray = (position) => {
  if (!position) return null
  if (Array.isArray(position)) return position
  if (isFinite(position.lng) && isFinite(position.lat)) return [position.lng, position.lat]
  if (typeof position.getLng === 'function' && typeof position.getLat === 'function') {
    return [position.getLng(), position.getLat()]
  }
  return null
}

/**
 * 高德地图管理 Composable
 * @param {Object} config - 地图配置 { amapKey, amapSecurityJsCode }
 * @param {Object} baseMapProfile - 底图 profile，coordinate_policy=gcj02 时只在展示边界偏移
 */
export function useGaodeMap(config, baseMapProfile = {}) {
  const mapInstance = ref(null)
  const amapLib = ref(null)
  const overlays = ref([])
  const infoWindow = ref(null)
  const featureOverlayMap = new Map()
  const featureDataMap = new Map()
  let highlightedOverlays = []

  let eventsBound = false
  let viewState = { center: mapDisplayCoordinate(DEFAULT_CENTER, baseMapProfile), zoom: 4 }

  const toDisplayCoordinate = (coordinate) => mapDisplayCoordinate(coordinate, baseMapProfile)
  const toSourceCoordinate = (coordinate) => mapSourceCoordinate(coordinate, baseMapProfile)
  const toDisplayPath = (path) => path.map((coord) => toDisplayCoordinate(coord))
  const emitFeatureClick = (callback, feature, displayPosition) => {
    if (!callback) return
    const displayCoordinate = positionToArray(displayPosition)
    callback(feature, toSourceCoordinate(displayCoordinate), displayPosition)
  }

  const updateViewState = () => {
    if (!mapInstance.value) return
    const center = mapInstance.value.getCenter?.()
    const zoom = mapInstance.value.getZoom?.()
    if (center && isFinite(center.lng) && isFinite(center.lat) && isFinite(zoom)) {
      viewState = {
        center: [center.lng, center.lat],
        zoom
      }
    }
  }

  const bindEvents = () => {
    if (!mapInstance.value || eventsBound || !mapInstance.value.on) return
    mapInstance.value.on('moveend', updateViewState)
    mapInstance.value.on('zoomend', updateViewState)
    eventsBound = true
  }

  const applyViewState = () => {
    if (!mapInstance.value || !amapLib.value) return
    const [lng, lat] = viewState.center
    if (!isFinite(lng) || !isFinite(lat)) return
    const zoom = isFinite(viewState.zoom) ? viewState.zoom : 4
    mapInstance.value.setZoomAndCenter(zoom, new amapLib.value.LngLat(lng, lat))
  }

  const initMap = async (container) => {
    if (!config.amapKey) {
      ElMessage.warning('未配置高德地图 Key，无法加载高德底图')
      return null
    }

    if (config.amapSecurityJsCode && typeof window !== 'undefined') {
      window._AMapSecurityConfig = {
        ...(window._AMapSecurityConfig || {}),
        securityJsCode: config.amapSecurityJsCode
      }
    }

    if (!amapLib.value) {
      try {
        amapLib.value = await AMapLoader.load({
          key: config.amapKey,
          version: '2.0',
          plugins: ['AMap.Scale', 'AMap.ToolBar', 'AMap.CircleMarker']
        })
      } catch (error) {
        console.error('高德地图加载失败', error)
        ElMessage.error('高德底图加载失败，请检查网络或密钥配置')
        return null
      }
    }

    if (!container) return null

    const initialCenter = viewState?.center && isFinite(viewState.center[0]) && isFinite(viewState.center[1])
      ? viewState.center
      : mapDisplayCoordinate(DEFAULT_CENTER, baseMapProfile)
    const initialZoom = viewState && isFinite(viewState.zoom) ? viewState.zoom : 4

    if (!mapInstance.value) {
      container.innerHTML = ''
      mapInstance.value = new amapLib.value.Map(container, {
        viewMode: '2D',
        zoom: initialZoom,
        center: initialCenter,
        mapStyle: 'amap://styles/normal',
        pitch: 0,
        showLabel: true
      })

      if (amapLib.value.Scale) {
        mapInstance.value.addControl(new amapLib.value.Scale())
      }
      if (amapLib.value.ToolBar) {
        mapInstance.value.addControl(new amapLib.value.ToolBar())
      }
      infoWindow.value = new amapLib.value.InfoWindow({
        offset: new amapLib.value.Pixel(0, -20)
      })
    } else if (initialCenter && mapInstance.value.setZoomAndCenter) {
      mapInstance.value.setZoomAndCenter(initialZoom, initialCenter)
    }

    bindEvents()

    return {
      AMap: amapLib.value,
      map: mapInstance.value
    }
  }

  const createMarker = (lng, lat) => {
    if (!isFinite(lng) || !isFinite(lat) || !amapLib.value) return null
    const [displayLng, displayLat] = toDisplayCoordinate([lng, lat])

    if (amapLib.value.CircleMarker) {
      const marker = new amapLib.value.CircleMarker({
        center: [displayLng, displayLat],
        ...POINT_STYLE
      })
      marker.__addpDefaultStyle = POINT_STYLE
      marker.__addpHighlightStyle = POINT_HIGHLIGHT_STYLE
      return marker
    }

    const div = document.createElement('div')
    div.className = 'gaode-point-marker'
    const marker = new amapLib.value.Marker({
      position: [displayLng, displayLat],
      offset: new amapLib.value.Pixel(-6, -6),
      content: div
    })
    marker.__addpDefaultClassName = 'gaode-point-marker'
    marker.__addpHighlightClassName = 'gaode-point-marker is-highlighted'
    return marker
  }

  const createPolygon = (rings) => {
    if (!amapLib.value) return null
    const polygon = new amapLib.value.Polygon({
      path: rings,
      ...POLYGON_STYLE
    })
    polygon.__addpDefaultStyle = POLYGON_STYLE
    polygon.__addpHighlightStyle = POLYGON_HIGHLIGHT_STYLE
    return polygon
  }

  const createPolyline = (path) => {
    if (!amapLib.value) return null
    const polyline = new amapLib.value.Polyline({
      path,
      ...LINE_STYLE
    })
    polyline.__addpDefaultStyle = LINE_STYLE
    polyline.__addpHighlightStyle = LINE_HIGHLIGHT_STYLE
    return polyline
  }

  const registerFeatureOverlay = (feature, overlay) => {
    if (!feature || !overlay) return
    const rowKey =
      feature?.properties?.__rowKey ||
      feature?.properties?.id ||
      feature?.properties?.ID ||
      feature?.id
    if (!rowKey) return
    if (!featureOverlayMap.has(rowKey)) {
      featureOverlayMap.set(rowKey, [])
    }
    featureOverlayMap.get(rowKey).push(overlay)
    featureDataMap.set(rowKey, feature)
  }

  const renderFeatures = (features, options = {}) => {
    if (!mapInstance.value || !amapLib.value) return

    // 清除现有覆盖物
    clearOverlays()
    featureOverlayMap.clear()
    featureDataMap.clear()

    const newOverlays = []

    features.forEach((feature) => {
      const geometry = feature?.geometry
      if (!geometry?.type || !geometry.coordinates) return

      switch (geometry.type) {
        case 'Point': {
          const marker = createMarker(geometry.coordinates[0], geometry.coordinates[1])
          if (marker) {
            newOverlays.push(marker)
            registerFeatureOverlay(feature, marker)
            if (options.onFeatureClick) {
              marker.on('click', () => {
                highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                emitFeatureClick(options.onFeatureClick, feature, marker.getPosition())
              })
            }
          }
          break
        }
        case 'MultiPoint': {
          geometry.coordinates.forEach((coord) => {
            const marker = createMarker(coord[0], coord[1])
            if (marker) {
              newOverlays.push(marker)
              registerFeatureOverlay(feature, marker)
              if (options.onFeatureClick) {
                marker.on('click', () => {
                  highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                  emitFeatureClick(options.onFeatureClick, feature, marker.getPosition())
                })
              }
            }
          })
          break
        }
        case 'LineString': {
          const path = toDisplayPath(geometry.coordinates)
          const polyline = createPolyline(path)
          if (polyline) {
            newOverlays.push(polyline)
            registerFeatureOverlay(feature, polyline)
            if (options.onFeatureClick) {
              polyline.on('click', (e) => {
                highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                emitFeatureClick(options.onFeatureClick, feature, e.lnglat)
              })
            }
          }
          break
        }
        case 'MultiLineString': {
          geometry.coordinates.forEach((line) => {
            const path = toDisplayPath(line)
            const polyline = createPolyline(path)
            if (polyline) {
              newOverlays.push(polyline)
              registerFeatureOverlay(feature, polyline)
              if (options.onFeatureClick) {
                polyline.on('click', (e) => {
                  highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                  emitFeatureClick(options.onFeatureClick, feature, e.lnglat)
                })
              }
            }
          })
          break
        }
        case 'Polygon': {
          const rings = geometry.coordinates.map((ring) => toDisplayPath(ring))
          const polygon = createPolygon(rings)
          if (polygon) {
            newOverlays.push(polygon)
            registerFeatureOverlay(feature, polygon)
            if (options.onFeatureClick) {
              polygon.on('click', (e) => {
                highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                emitFeatureClick(options.onFeatureClick, feature, e.lnglat)
              })
            }
          }
          break
        }
        case 'MultiPolygon': {
          geometry.coordinates.forEach((polygonCoords) => {
            const rings = polygonCoords.map((ring) => toDisplayPath(ring))
            const polygon = createPolygon(rings)
            if (polygon) {
              newOverlays.push(polygon)
              registerFeatureOverlay(feature, polygon)
              if (options.onFeatureClick) {
                polygon.on('click', (e) => {
                  highlightFeatureByKey(feature?.properties?.__rowKey || feature?.properties?.id || feature?.properties?.ID || feature?.id)
                  emitFeatureClick(options.onFeatureClick, feature, e.lnglat)
                })
              }
            }
          })
          break
        }
      }
    })

    if (newOverlays.length === 0) {
      if (!options.preserveView) {
        const defaultCenter = mapDisplayCoordinate(DEFAULT_CENTER, baseMapProfile)
        mapInstance.value.setZoomAndCenter(4, defaultCenter)
        viewState = { center: defaultCenter, zoom: 4 }
      } else {
        updateViewState()
      }
      return
    }

    mapInstance.value.add(newOverlays)
    overlays.value = newOverlays

    if (!options.preserveView) {
      mapInstance.value.setFitView(newOverlays, false, [20, 20, 20, 20])
      setTimeout(updateViewState, 0)
    } else {
      updateViewState()
    }
  }

  const focusFeature = (rowKey, options = {}) => {
    if (!mapInstance.value || !rowKey) return false
    const feature = featureDataMap.get(rowKey)
    if (!feature) return false

    const overlaysForFeature = featureOverlayMap.get(rowKey) || []
    const geometry = feature.geometry
    const shouldFit = options.fit !== false
    const padding = options.padding || [40, 40, 40, 40]
    const minZoom = options.minZoom || 8

    highlightFeatureByKey(rowKey)

    if (shouldFit && overlaysForFeature.length > 0 && mapInstance.value.setFitView) {
      mapInstance.value.setFitView(overlaysForFeature, false, padding)
      setTimeout(updateViewState, 0)
    } else {
      const sourceCenter = options.center || getGeometryCenter(geometry)
      const center = Array.isArray(sourceCenter) ? toDisplayCoordinate(sourceCenter) : sourceCenter
      if (center && mapInstance.value.setZoomAndCenter) {
        const targetZoom = Math.max(mapInstance.value.getZoom?.() || minZoom, minZoom)
        mapInstance.value.setZoomAndCenter(targetZoom, center)
        setTimeout(updateViewState, 0)
      }
    }

    if (options.openPopup && options.popupContent) {
      const content =
        typeof options.popupContent === 'function'
          ? options.popupContent(feature.properties || {})
          : options.popupContent
      if (content) {
        const popupPosition = options.position || options.coordinate || getGeometryCenter(geometry)
        showPopup(content, popupPosition)
      }
    } else if (!options.keepPopup) {
      hidePopup()
    }

    return true
  }

  const setOverlayStyle = (overlay, style) => {
    if (!overlay || !style) return
    if (style.className && typeof overlay.setContent === 'function') {
      const div = document.createElement('div')
      div.className = style.className || overlay.__addpDefaultClassName || 'gaode-point-marker'
      overlay.setContent(div)
    } else if (typeof overlay.setOptions === 'function') {
      overlay.setOptions({ ...style })
    }
  }

  const clearHighlight = () => {
    highlightedOverlays.forEach((overlay) => {
      if (overlay?.__addpDefaultStyle) {
        setOverlayStyle(overlay, overlay.__addpDefaultStyle)
      } else if (overlay?.__addpDefaultClassName) {
        setOverlayStyle(overlay, { className: overlay.__addpDefaultClassName })
      }
    })
    highlightedOverlays = []
  }

  const highlightFeatureByKey = (rowKey) => {
    if (!rowKey) return
    const overlaysForFeature = featureOverlayMap.get(rowKey) || []
    clearHighlight()
    overlaysForFeature.forEach((overlay) => {
      if (overlay?.__addpHighlightStyle) {
        setOverlayStyle(overlay, overlay.__addpHighlightStyle)
      } else if (overlay?.__addpHighlightClassName) {
        setOverlayStyle(overlay, { className: overlay.__addpHighlightClassName })
      }
    })
    highlightedOverlays = overlaysForFeature
  }

  const clearOverlays = () => {
    clearHighlight()
    if (overlays.value.length > 0) {
      overlays.value.forEach((overlay) => {
        if (overlay?.setMap) {
          overlay.setMap(null)
        } else if (overlay?.destroy) {
          overlay.destroy()
        }
      })
      overlays.value = []
    }
    if (infoWindow.value) {
      infoWindow.value.close()
    }
    featureOverlayMap.clear()
    featureDataMap.clear()
  }

  const showPopup = (content, position) => {
    if (!infoWindow.value || !mapInstance.value || !amapLib.value) return

    infoWindow.value.setContent(content)

    let lngLatPosition = position
    if (Array.isArray(position)) {
      const [displayLng, displayLat] = toDisplayCoordinate(position)
      lngLatPosition = new amapLib.value.LngLat(displayLng, displayLat)
    }

    if (lngLatPosition) {
      infoWindow.value.open(mapInstance.value, lngLatPosition)
    }
  }

  const hidePopup = () => {
    if (infoWindow.value) {
      infoWindow.value.close()
    }
  }

  const destroy = () => {
    clearOverlays()

    if (eventsBound && mapInstance.value?.off) {
      mapInstance.value.off('moveend', updateViewState)
      mapInstance.value.off('zoomend', updateViewState)
      eventsBound = false
    }

    if (mapInstance.value?.destroy) {
      mapInstance.value.destroy()
    }

    mapInstance.value = null
    if (infoWindow.value) {
      infoWindow.value.close()
      infoWindow.value = null
    }
  }

  onBeforeUnmount(() => {
    destroy()
  })

  return {
    mapInstance,
    amapLib,
    initMap,
    renderFeatures,
    focusFeature,
    clearOverlays,
    showPopup,
    hidePopup,
    updateViewState,
    applyViewState,
    destroy
  }
}
