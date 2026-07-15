import { describe, expect, it } from 'vitest'
import * as THREE from 'three'
import { s3mCameraFitDistanceForBox } from '@/lib/supermap-s3m/three/S3MThreeCamera.js'

describe('S3M Three.js camera fit', () => {
  it('fits all box corners against both viewport field-of-view limits', () => {
    const box = new THREE.Box3(
      new THREE.Vector3(-1, -1, -1),
      new THREE.Vector3(1, 1, 1)
    )
    const direction = new THREE.Vector3(0, -1, 0)
    const up = new THREE.Vector3(0, 0, 1)

    expect(s3mCameraFitDistanceForBox(box, direction, up, 90, 1, 1)).toBeCloseTo(2)
    expect(s3mCameraFitDistanceForBox(box, direction, up, 90, 0.5, 1)).toBeCloseTo(3)
    expect(s3mCameraFitDistanceForBox(box, direction, up, 90, 1)).toBeGreaterThan(2)
  })

  it('rejects invalid boxes and camera parameters', () => {
    expect(s3mCameraFitDistanceForBox(new THREE.Box3(), new THREE.Vector3(1, 0, 0), new THREE.Vector3(0, 0, 1), 45, 1)).toBeNull()
    expect(s3mCameraFitDistanceForBox(
      new THREE.Box3(new THREE.Vector3(-1, -1, -1), new THREE.Vector3(1, 1, 1)),
      new THREE.Vector3(0, 0, 1),
      new THREE.Vector3(0, 0, 1),
      45,
      1
    )).toBeNull()
  })
})
