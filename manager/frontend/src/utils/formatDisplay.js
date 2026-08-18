const formatDisplayNames = {
  '3dtiles': '3D Tiles',
  glb: 'GLB',
  gltf: 'glTF',
  splat: 'Splat',
  ksplat: 'KSplat',
  las: 'LAS',
  laz: 'LAZ',
  copc: 'COPC',
  e57: 'E57',
  pcd: 'PCD',
  xyz: 'XYZ',
  geojson: 'GeoJSON',
  access: 'Microsoft Access Database',
  pgeo: 'Personal Geodatabase',
  geotiff: 'GeoTIFF',
  tiff: 'TIFF',
  tif: 'TIFF',
  raster_mosaic: 'Raster Mosaic',
  udbx: 'SuperMap UDBX'
}

export function dataFormatDisplayName(value) {
  const normalized = String(value || '').trim().toLowerCase()
  if (!normalized) return ''
  if (formatDisplayNames[normalized]) return formatDisplayNames[normalized]
  return normalized.toUpperCase()
}
