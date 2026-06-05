import proj4 from 'proj4'
import { register } from 'ol/proj/proj4'

const WGS84 = 'EPSG:4326'
const WEB_MERCATOR = 'EPSG:3857'
const SUPPORTED_DEFINITION_ENCODINGS = new Set(['wkt', 'esri_wkt', 'proj4'])

let registered = false

const ensureBuiltIns = () => {
  if (registered) return
  proj4.defs(WGS84, '+proj=longlat +datum=WGS84 +no_defs +type=crs')
  proj4.defs(
    WEB_MERCATOR,
    '+proj=merc +a=6378137 +b=6378137 +lat_ts=0 +lon_0=0 +x_0=0 +y_0=0 +k=1 +units=m +nadgrids=@null +wktext +no_defs +type=crs'
  )
  register(proj4)
  registered = true
}

const numberValue = (value) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

const metadata = (preview) => {
  return preview?.object?.content?.metadata || preview?.content?.metadata || {}
}

export const sourceSRIDFromPreview = (preview) => {
  const direct = numberValue(preview?.source_srid || preview?.srid)
  if (direct > 0) return direct

  const meta = metadata(preview)
  const fromMeta = numberValue(meta?.source_srid || meta?.srid)
  if (fromMeta > 0) return fromMeta

  const spatialRef = String(meta?.source_crs || meta?.spatial_ref_sys || '').trim()
  const match = spatialRef.match(/^EPSG:(\d+)$/i)
  return match ? numberValue(match[1]) : 0
}

export const sourceCRSFromPreview = (preview) => {
  const direct = String(preview?.source_crs || '').trim()
  if (direct) return direct
  const meta = metadata(preview)
  return String(meta?.source_crs || meta?.spatial_ref_sys || '').trim()
}

export const sourceCRSDefinitionFromPreview = (preview) => {
  const direct = preview?.source_crs_definition
  if (direct && typeof direct === 'object') return direct
  const metaDefinition = metadata(preview)?.source_crs_definition
  if (metaDefinition && typeof metaDefinition === 'object') return metaDefinition
  return null
}

const sourceCodeFromPreview = (preview) => {
  const targetSRID = numberValue(preview?.target_srid)
  if (targetSRID === 4326) return WGS84

  const crs = sourceCRSFromPreview(preview)
  if (crs) {
    const epsgMatch = crs.match(/^EPSG:(\d+)$/i)
    return epsgMatch ? `EPSG:${epsgMatch[1]}` : crs
  }

  const srid = sourceSRIDFromPreview(preview)
  if (srid > 0) return `EPSG:${srid}`

  return ''
}

const canRegisterCRSDefinition = (code, crsDefinition) => {
  if (!code || !crsDefinition || typeof crsDefinition !== 'object') return false

  const encoding = String(crsDefinition.definition_encoding || '').trim().toLowerCase()
  const definition = String(crsDefinition.definition || '').trim()
  if (!SUPPORTED_DEFINITION_ENCODINGS.has(encoding) || !definition) return false
  if (/^EPSG:\d+$/i.test(definition)) return false

  try {
    proj4.defs(code, definition)
    register(proj4)
    return !!proj4.defs(code)
  } catch (_error) {
    return false
  }
}

const ensureProjection = (code, crsDefinition) => {
  ensureBuiltIns()
  if (!code) return false
  if (proj4.defs(code)) return true
  return canRegisterCRSDefinition(code, crsDefinition)
}

export const getPreviewCRSTransform = (preview) => {
  ensureBuiltIns()

  const transformStatus = String(preview?.transform_status || metadata(preview)?.transform_status || '').trim()
  if (transformStatus === 'unknown_crs') {
    return { status: 'unknown_crs', message: preview?.transform_message || metadata(preview)?.transform_message || '' }
  }

  const sourceCode = sourceCodeFromPreview(preview)
  if (!sourceCode) {
    return { status: 'unknown_crs', message: preview?.transform_message || metadata(preview)?.transform_message || '' }
  }
  if (sourceCode.toUpperCase() === WGS84) {
    return { status: 'direct', sourceCode, targetCode: WGS84 }
  }
  const sourceCRSDefinition = sourceCRSDefinitionFromPreview(preview)
  if (!ensureProjection(sourceCode, sourceCRSDefinition)) {
    return { status: 'unsupported_crs', sourceCode, targetCode: WGS84 }
  }

  return {
    status: 'transformable',
    sourceCode,
    targetCode: WGS84,
    transformCoordinate: (coordinate) => proj4(sourceCode, WGS84, coordinate)
  }
}

const transformCoordinates = (value, transformCoordinate) => {
  if (!Array.isArray(value)) return value
  if (value.length >= 2 && typeof value[0] === 'number' && typeof value[1] === 'number') {
    const transformed = transformCoordinate([value[0], value[1]])
    return [
      transformed[0],
      transformed[1],
      ...value.slice(2)
    ]
  }
  return value.map((item) => transformCoordinates(item, transformCoordinate))
}

export const transformGeoJSONGeometryToWGS84 = (geometry, crsTransform) => {
  if (!geometry || typeof geometry !== 'object') return geometry
  if (!crsTransform || crsTransform.status === 'direct') return geometry
  if (crsTransform.status !== 'transformable' || typeof crsTransform.transformCoordinate !== 'function') {
    return null
  }

  if (geometry.type === 'GeometryCollection') {
    return {
      ...geometry,
      geometries: Array.isArray(geometry.geometries)
        ? geometry.geometries.map((item) => transformGeoJSONGeometryToWGS84(item, crsTransform)).filter(Boolean)
        : []
    }
  }

  return {
    ...geometry,
    coordinates: transformCoordinates(geometry.coordinates, crsTransform.transformCoordinate)
  }
}

export const crsSuppressionStatus = (crsTransform) => {
  if (!crsTransform) return ''
  if (crsTransform.status === 'unknown_crs') return 'unknown_crs'
  if (crsTransform.status === 'unsupported_crs') return 'unsupported_crs'
  return ''
}
