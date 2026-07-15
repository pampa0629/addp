import * as THREE from 'three'

export function s3mCameraFitDistanceForBox(
  box,
  cameraDirection,
  cameraUp,
  verticalFovDegrees,
  aspect,
  margin = 1.12
) {
  if (!box || box.isEmpty()) return null
  const verticalFov = THREE.MathUtils.degToRad(Number(verticalFovDegrees))
  const normalizedAspect = Number(aspect)
  const normalizedMargin = Number(margin)
  if (
    !Number.isFinite(verticalFov) || verticalFov <= 0 || verticalFov >= Math.PI ||
    !Number.isFinite(normalizedAspect) || normalizedAspect <= 0 ||
    !Number.isFinite(normalizedMargin) || normalizedMargin < 1
  ) return null

  const direction = cameraDirection.clone().normalize()
  const right = new THREE.Vector3().crossVectors(cameraUp, direction).normalize()
  if (right.lengthSq() === 0) return null
  const viewUp = new THREE.Vector3().crossVectors(direction, right).normalize()
  const center = box.getCenter(new THREE.Vector3())
  const min = box.min
  const max = box.max
  const tanVertical = Math.tan(verticalFov / 2)
  const tanHorizontal = tanVertical * normalizedAspect
  let distance = 0

  for (const x of [min.x, max.x]) {
    for (const y of [min.y, max.y]) {
      for (const z of [min.z, max.z]) {
        const relative = new THREE.Vector3(x, y, z).sub(center)
        const forwardOffset = relative.dot(direction)
        distance = Math.max(
          distance,
          forwardOffset + Math.abs(relative.dot(right)) * normalizedMargin / tanHorizontal,
          forwardOffset + Math.abs(relative.dot(viewUp)) * normalizedMargin / tanVertical,
          forwardOffset + 0.01
        )
      }
    }
  }
  return distance
}
