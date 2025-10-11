import { ref, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import AMapLoader from '@amap/amap-jsapi-loader'

const DEFAULT_CENTER = [104.0668, 30.5728]

/**
 * 高德地图管理 Composable
 * @param {Object} config - 地图配置 { amapKey, amapSecurityJsCode }
 */
export function useGaodeMap(config) {
  const mapInstance = ref(null)
  const amapLib = ref(null)
  const overlays = ref([])
  const infoWindow = ref(null)

  let eventsBound = false
  let viewState = { center: DEFAULT_CENTER, zoom: 4 }

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
      : DEFAULT_CENTER
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

    if (amapLib.value.CircleMarker) {
      return new amapLib.value.CircleMarker({
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
    return new amapLib.value.Marker({
      position: [lng, lat],
      offset: new amapLib.value.Pixel(-6, -6),
      content: div
    })
  }

  const createPolygon = (rings) => {
    if (!amapLib.value) return null
    return new amapLib.value.Polygon({
      path: rings,
      strokeColor: '#67C23A',
      strokeWeight: 2,
      strokeOpacity: 0.8,
      fillColor: '#67C23A',
      fillOpacity: 0.25
    })
  }

  const createPolyline = (path) => {
    if (!amapLib.value) return null
    return new amapLib.value.Polyline({
      path,
      strokeColor: '#409EFF',
      strokeWeight: 3,
      strokeOpacity: 0.9
    })
  }

  const renderFeatures = (features, options = {}) => {
    if (!mapInstance.value || !amapLib.value) return

    // 清除现有覆盖物
    clearOverlays()

    const newOverlays = []

    features.forEach((feature) => {
      const geometry = feature?.geometry
      if (!geometry?.type || !geometry.coordinates) return

      switch (geometry.type) {
        case 'Point': {
          const marker = createMarker(geometry.coordinates[0], geometry.coordinates[1])
          if (marker) {
            newOverlays.push(marker)
            if (options.onFeatureClick) {
              marker.on('click', () => options.onFeatureClick(feature, marker.getPosition()))
            }
          }
          break
        }
        case 'MultiPoint': {
          geometry.coordinates.forEach((coord) => {
            const marker = createMarker(coord[0], coord[1])
            if (marker) {
              newOverlays.push(marker)
              if (options.onFeatureClick) {
                marker.on('click', () => options.onFeatureClick(feature, marker.getPosition()))
              }
            }
          })
          break
        }
        case 'LineString': {
          const path = geometry.coordinates.map(([lng, lat]) => [lng, lat])
          const polyline = createPolyline(path)
          if (polyline) {
            newOverlays.push(polyline)
            if (options.onFeatureClick) {
              polyline.on('click', (e) => options.onFeatureClick(feature, e.lnglat))
            }
          }
          break
        }
        case 'MultiLineString': {
          geometry.coordinates.forEach((line) => {
            const path = line.map(([lng, lat]) => [lng, lat])
            const polyline = createPolyline(path)
            if (polyline) {
              newOverlays.push(polyline)
              if (options.onFeatureClick) {
                polyline.on('click', (e) => options.onFeatureClick(feature, e.lnglat))
              }
            }
          })
          break
        }
        case 'Polygon': {
          const rings = geometry.coordinates.map((ring) => ring.map(([lng, lat]) => [lng, lat]))
          const polygon = createPolygon(rings)
          if (polygon) {
            newOverlays.push(polygon)
            if (options.onFeatureClick) {
              polygon.on('click', (e) => options.onFeatureClick(feature, e.lnglat))
            }
          }
          break
        }
        case 'MultiPolygon': {
          geometry.coordinates.forEach((polygonCoords) => {
            const rings = polygonCoords.map((ring) => ring.map(([lng, lat]) => [lng, lat]))
            const polygon = createPolygon(rings)
            if (polygon) {
              newOverlays.push(polygon)
              if (options.onFeatureClick) {
                polygon.on('click', (e) => options.onFeatureClick(feature, e.lnglat))
              }
            }
          })
          break
        }
      }
    })

    if (newOverlays.length === 0) {
      if (!options.preserveView) {
        mapInstance.value.setZoomAndCenter(4, DEFAULT_CENTER)
        viewState = { center: DEFAULT_CENTER, zoom: 4 }
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

  const clearOverlays = () => {
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
  }

  const showPopup = (content, position) => {
    if (!infoWindow.value || !mapInstance.value || !amapLib.value) return

    infoWindow.value.setContent(content)

    let lngLatPosition = position
    if (Array.isArray(position)) {
      lngLatPosition = new amapLib.value.LngLat(position[0], position[1])
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
    clearOverlays,
    showPopup,
    hidePopup,
    updateViewState,
    applyViewState,
    destroy
  }
}
