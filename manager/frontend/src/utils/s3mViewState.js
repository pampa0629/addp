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
