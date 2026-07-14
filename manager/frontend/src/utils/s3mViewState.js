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
