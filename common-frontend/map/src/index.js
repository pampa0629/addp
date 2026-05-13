// Map-related Components (requires ol and @amap/amap-jsapi-loader)
export { default as GeoJsonPreview } from './components/GeoJsonPreview.vue'
export { default as TablePreview } from './components/TablePreview.vue'
export { default as TilePreview } from './components/TilePreview.vue'
export { default as MapContainer } from './components/map/MapContainer.vue'
export { default as GaodeMapRenderer } from './components/map/GaodeMapRenderer.vue'
export { default as OpenLayersRenderer } from './components/map/OpenLayersRenderer.vue'
export { default as ExtractedMetadata } from './components/ExtractedMetadata.vue'
export { default as MapPopup } from './components/MapPopup.vue'

// Map Composables
export { useMapConfig, setMapConfigAPI } from './composables/useMapConfig'
export { useGaodeMap } from './composables/useGaodeMap'
export { useOpenLayersMap } from './composables/useOpenLayersMap'
export { useResizable } from './composables/useResizable'
export { useMapPopup } from './composables/useMapPopup'
export { useFeatureHighlight } from './composables/useFeatureHighlight'
export { useVectorTileLoader } from './composables/useVectorTileLoader'

// Utils
export { formatBytes, formatDate, safeStringify } from './utils/formatters'
export { formatFeatureProperties } from './utils/mapFormatters'
export { fromLonLat, toLonLat } from './utils/mapProjection'
export { createDefaultStyleFunction, createHighlightStyle } from './utils/mapStyles'

// Map Layers Config
export { createGaodeBaseLayer, createOSMBaseLayer } from './config/mapLayers'

// i18n messages (for merging into host app's i18n instance)
export { default as mapMessagesZhCn } from './i18n/zh-cn.json'
export { default as mapMessagesEn } from './i18n/en.json'
