import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import * as THREE from 'three'
import { cameraFitDistanceForBox } from '@/utils/scene3dCamera.js'

describe('Manager Three.js camera fit', () => {
  it.each([0.525, 1, 1.6])('keeps separated model parts within viewport aspect %s', (aspect) => {
    const box = new THREE.Box3(new THREE.Vector3(-19, -3, -12), new THREE.Vector3(20, 4, 13))
    const direction = new THREE.Vector3(0.75, 0.55, 1.15).normalize()
    const target = box.getCenter(new THREE.Vector3()).add(new THREE.Vector3(-1.5, -0.2, -1))
    const distance = cameraFitDistanceForBox(box, direction, new THREE.Vector3(0, 1, 0), 45, aspect, 1.12, target)
    const camera = new THREE.PerspectiveCamera(45, aspect, 0.01, 1000)
    camera.position.copy(target).addScaledVector(direction, distance)
    camera.lookAt(target)
    camera.updateMatrixWorld()
    for (const x of [box.min.x, box.max.x]) {
      for (const y of [box.min.y, box.max.y]) {
        for (const z of [box.min.z, box.max.z]) {
          const projected = new THREE.Vector3(x, y, z).project(camera)
          expect(Math.abs(projected.x)).toBeLessThan(1)
          expect(Math.abs(projected.y)).toBeLessThan(1)
          expect(projected.z).toBeGreaterThan(-1)
          expect(projected.z).toBeLessThan(1)
        }
      }
    }
  })

  it('uses the same camera fitting owner for model and S3M previews', () => {
    for (const name of ['Model3DPreview', 'S3MPreview']) {
      const source = readFileSync(new URL(`../../src/components/explorer/${name}.vue`, import.meta.url), 'utf8')
      expect(source).toContain("import { cameraFitDistanceForBox } from '@/utils/scene3dCamera.js'")
      expect(source).toContain('cameraFitDistanceForBox(')
      expect(source).not.toContain('maxDim / (2 * Math.tan(')
    }
  })

  it('fits all box corners against both viewport field-of-view limits', () => {
    const box = new THREE.Box3(
      new THREE.Vector3(-1, -1, -1),
      new THREE.Vector3(1, 1, 1)
    )
    const direction = new THREE.Vector3(0, -1, 0)
    const up = new THREE.Vector3(0, 0, 1)

    expect(cameraFitDistanceForBox(box, direction, up, 90, 1, 1)).toBeCloseTo(2)
    expect(cameraFitDistanceForBox(box, direction, up, 90, 0.5, 1)).toBeCloseTo(3)
    expect(cameraFitDistanceForBox(box, direction, up, 90, 1)).toBeGreaterThan(2)
  })

  it('rejects invalid boxes and camera parameters', () => {
    expect(cameraFitDistanceForBox(new THREE.Box3(), new THREE.Vector3(1, 0, 0), new THREE.Vector3(0, 0, 1), 45, 1)).toBeNull()
    expect(cameraFitDistanceForBox(
      new THREE.Box3(new THREE.Vector3(-1, -1, -1), new THREE.Vector3(1, 1, 1)),
      new THREE.Vector3(0, 0, 1),
      new THREE.Vector3(0, 0, 1),
      45,
      1
    )).toBeNull()
  })
})
