const formatDisplayNames = {
  '3dtiles': '3D Tiles',
  glb: 'GLB',
  gltf: 'glTF',
  las: 'LAS',
  laz: 'LAZ',
  geojson: 'GeoJSON',
  geotiff: 'GeoTIFF',
  tiff: 'TIFF',
  tif: 'TIFF',
  raster_mosaic: 'Raster Mosaic'
}

export function dataFormatDisplayName(value) {
  const normalized = String(value || '').trim().toLowerCase()
  if (!normalized) return ''
  if (formatDisplayNames[normalized]) return formatDisplayNames[normalized]
  return normalized.toUpperCase()
}
