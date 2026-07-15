export const S3M_VIEW_MODE_MODEL = 'model'
export const S3M_VIEW_MODE_GLOBE = 'globe'
export const S3M_THREE_RENDERER_RUNTIME = 'three_s3m'

export function isThreeS3MViewState(state) {
  return Boolean(state && typeof state === 'object' && state.renderer_runtime === S3M_THREE_RENDERER_RUNTIME)
}

export function normalizeS3MViewMode(value) {
  return value === S3M_VIEW_MODE_GLOBE ? S3M_VIEW_MODE_GLOBE : S3M_VIEW_MODE_MODEL
}

export function isS3MViewStateForMode(state, viewMode) {
  if (!state || typeof state !== 'object') return false
  if (![S3M_VIEW_MODE_MODEL, S3M_VIEW_MODE_GLOBE].includes(state.view_mode)) return false
  return state.view_mode === normalizeS3MViewMode(viewMode)
}

export function s3mGlobeCameraRange(radius) {
  const normalizedRadius = Number(radius)
  return Math.max(Number.isFinite(normalizedRadius) && normalizedRadius > 0 ? normalizedRadius * 5 : 0, 5000)
}

export function cameraViewState(camera) {
  const position = camera?.positionWC
  const coordinates = [Number(position?.x), Number(position?.y), Number(position?.z)]
  if (!coordinates.every(Number.isFinite)) return null
  return {
    position: coordinates,
    heading: Number(camera?.heading) || 0,
    pitch: Number(camera?.pitch) || 0,
    roll: Number(camera?.roll) || 0
  }
}

export function preserveDerivedResourceQuery(resource) {
  if (!resource || typeof resource.getDerivedResource !== 'function') return resource
  const getDerivedResource = resource.getDerivedResource.bind(resource)
  resource.getDerivedResource = (options = {}) => getDerivedResource({
    ...options,
    preserveQueryParameters: true
  })
  return resource
}

export function s3mRootBoundingSphere(Cesium, rootTiles) {
  const spheres = (Array.isArray(rootTiles) ? rootTiles : []).map((tile) => {
    const volume = tile?.boundingVolume?.boundingVolume
    if (!volume) return null
    if (Number.isFinite(Number(volume.radius))) {
      return Cesium.BoundingSphere.clone(volume)
    }
    if (volume.halfAxes) {
      return Cesium.BoundingSphere.fromOrientedBoundingBox(volume)
    }
    return null
  }).filter(Boolean)
  if (!spheres.length) return null
  return Cesium.BoundingSphere.fromBoundingSpheres(spheres)
}
