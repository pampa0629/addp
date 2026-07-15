import { describe, expect, it } from 'vitest'
import * as THREE from 'three'
import {
  decodeS3MAttribute,
  resolveS3MResourceURL,
  s3mRootTilesBoundingBox,
  s3mRootTilesBoundingSphere,
  splitDXTMipmaps
} from '@/lib/supermap-s3m/three/S3MThreeContent.js'

function compressedAttribute(name, typedArray, components, options = {}) {
  return {
    attrLocation: { [name]: 0 },
    vertexAttributes: [{
      typedArray,
      componentsPerAttribute: components,
      componentDatatype: 5122,
      normalize: false
    }],
    ...options
  }
}

describe('S3M Three.js resource URL', () => {
  it('inherits the result version for every child tile request', () => {
    expect(resolveS3MResourceURL(
      '..\\Data\\Tile_1.s3mb',
      'http://localhost/api/scene/config/root.s3mb?version=execution-7'
    )).toBe('http://localhost/api/scene/Data/Tile_1.s3mb?version=execution-7')
  })
})

describe('S3M Draco quantized attributes', () => {
  it('decodes quantized positions into model coordinates', () => {
    const vertexPackage = compressedAttribute('aPosition', new Int16Array([
      0, 10, 20,
      30, 40, 50
    ]), 3, {
      compressOptions: 1,
      vertCompressConstant: 0.5,
      minVerticesValue: { x: 100, y: 200, z: 300 }
    })

    expect(Array.from(decodeS3MAttribute(vertexPackage, 'aPosition').array)).toEqual([
      100, 205, 310,
      115, 220, 325
    ])
  })

  it('decodes quantized texture coordinates independently by axis', () => {
    const vertexPackage = compressedAttribute('aTexCoord0', new Int16Array([
      2, 4,
      6, 8
    ]), 2, {
      compressOptions: 16,
      texCoordCompressConstant: [{ x: 0.25, y: 0.5 }],
      minTexCoordValue: [{ x: -1, y: 2 }]
    })

    expect(Array.from(decodeS3MAttribute(vertexPackage, 'aTexCoord0').array)).toEqual([
      -0.5, 4,
      0.5, 6
    ])
  })
})

describe('S3M DXT mipmaps', () => {
  it('splits a non-square DXT1 mip chain by block dimensions', () => {
    const result = splitDXTMipmaps({
      internalFormat: THREE.RGB_S3TC_DXT1_Format,
      width: 8,
      height: 4,
      arrayBufferView: new Uint8Array(40)
    })

    expect(result.mipmaps.map(level => [level.width, level.height, level.data.byteLength])).toEqual([
      [8, 4, 16],
      [4, 2, 8],
      [2, 1, 8],
      [1, 1, 8]
    ])
  })
})

describe('S3M root bounds', () => {
  it('fits only stable root tile groups and includes hidden root content', () => {
    const left = new THREE.Group()
    const right = new THREE.Group()
    left.visible = false
    right.visible = false
    left.add(new THREE.Mesh(new THREE.BoxGeometry(2, 2, 2)))
    const rightMesh = new THREE.Mesh(new THREE.BoxGeometry(2, 2, 2))
    rightMesh.position.x = 10
    right.add(rightMesh)

    const sphere = s3mRootTilesBoundingSphere([
      { group: left },
      { group: right }
    ])
    const box = s3mRootTilesBoundingBox([
      { group: left },
      { group: right }
    ])

    expect(box.min.toArray()).toEqual([-1, -1, -1])
    expect(box.max.toArray()).toEqual([11, 1, 1])
    expect(sphere.center.x).toBeCloseTo(5)
    expect(sphere.radius).toBeCloseTo(Math.sqrt(38))
  })
})
