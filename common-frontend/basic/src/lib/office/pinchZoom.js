export function getTouchDistance(touches) {
  if (!touches || touches.length < 2) return 0
  const deltaX = Number(touches[1].clientX) - Number(touches[0].clientX)
  const deltaY = Number(touches[1].clientY) - Number(touches[0].clientY)
  if (!Number.isFinite(deltaX) || !Number.isFinite(deltaY)) return 0
  return Math.hypot(deltaX, deltaY)
}

function clampZoom(zoom, minimumZoom, maximumZoom) {
  return Number(Math.min(maximumZoom, Math.max(minimumZoom, zoom)).toFixed(2))
}

export function resolvePinchZoom(startZoom, startDistance, currentDistance, minimumZoom, maximumZoom) {
  const boundedStartZoom = clampZoom(startZoom, minimumZoom, maximumZoom)
  if (!Number.isFinite(startDistance) || startDistance <= 0 || !Number.isFinite(currentDistance)) {
    return boundedStartZoom
  }
  return clampZoom(startZoom * currentDistance / startDistance, minimumZoom, maximumZoom)
}

export function resolveWheelZoom(currentZoom, deltaY, minimumZoom, maximumZoom) {
  const boundedCurrentZoom = clampZoom(currentZoom, minimumZoom, maximumZoom)
  if (!Number.isFinite(deltaY) || deltaY === 0) return boundedCurrentZoom
  const boundedDelta = Math.min(20, Math.max(-20, deltaY))
  return clampZoom(currentZoom * Math.exp(-boundedDelta * 0.01), minimumZoom, maximumZoom)
}
