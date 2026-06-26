export const RASTER_QUICK_VIEW_GAODE_BASE_MAP = 'amapVector'
export const RASTER_QUICK_VIEW_TDT_VECTOR_BASE_MAP = 'tiandituVector'
export const RASTER_QUICK_VIEW_TDT_IMAGE_BASE_MAP = 'tiandituImage'

const TDT_BASE_MAP_ORDER = [
  RASTER_QUICK_VIEW_TDT_VECTOR_BASE_MAP,
  RASTER_QUICK_VIEW_TDT_IMAGE_BASE_MAP
]
const SUPPORTED_TDT_BASE_MAPS = new Set(TDT_BASE_MAP_ORDER)
const SUPPORTED_RASTER_BASE_MAPS = new Set([...SUPPORTED_TDT_BASE_MAPS, RASTER_QUICK_VIEW_GAODE_BASE_MAP])

export function rasterQuickViewBaseMapOptions(baseMapOptions = []) {
  const configuredOptions = Array.isArray(baseMapOptions)
    ? baseMapOptions.filter((item) => SUPPORTED_RASTER_BASE_MAPS.has(String(item?.value || '')))
    : []

  const byValue = new Map(configuredOptions.map((item) => [item.value, item]))
  const orderedOptions = [
    ...TDT_BASE_MAP_ORDER.map((value) => byValue.get(value)).filter(Boolean)
  ]
  const gaodeOption = byValue.get(RASTER_QUICK_VIEW_GAODE_BASE_MAP) ||
    { value: RASTER_QUICK_VIEW_GAODE_BASE_MAP, label: '高德地图 矢量（GCJ-02）' }

  return [...orderedOptions, gaodeOption]
}

export function defaultRasterQuickViewBaseMap(baseMapOptions = []) {
  return rasterQuickViewBaseMapOptions(baseMapOptions)[0]?.value || RASTER_QUICK_VIEW_GAODE_BASE_MAP
}

export function isTiandituRasterQuickViewBaseMap(value) {
  return SUPPORTED_TDT_BASE_MAPS.has(String(value || ''))
}

export function isGaodeRasterQuickViewBaseMap(value) {
  return String(value || '') === RASTER_QUICK_VIEW_GAODE_BASE_MAP
}
