import * as THREE from 'three'

const SVC_VERTEX = 1
const SVC_TEXTURE_COORD = 16
const DXT_FORMATS = new Set([
  THREE.RGB_S3TC_DXT1_Format,
  THREE.RGBA_S3TC_DXT1_Format,
  THREE.RGBA_S3TC_DXT3_Format,
  THREE.RGBA_S3TC_DXT5_Format
])

export function resolveS3MResourceURL(reference, baseURL) {
  const origin = globalThis.location?.origin || 'http://localhost'
  const base = new URL(baseURL, origin)
  const resolved = new URL(String(reference || '').replaceAll('\\', '/'), base)
  const version = base.searchParams.get('version')
  if (version && !resolved.searchParams.has('version')) {
    resolved.searchParams.set('version', version)
  }
  return resolved.href
}

function attributeArray(attribute) {
  const source = attribute?.typedArray
  if (!source) return null
  const buffer = source.buffer
  const byteOffset = source.byteOffset || 0
  const byteLength = source.byteLength
  switch (Number(attribute.componentDatatype)) {
    case 5120: return source instanceof Int8Array ? source : new Int8Array(buffer, byteOffset, byteLength)
    case 5121: return source instanceof Uint8Array ? source : new Uint8Array(buffer, byteOffset, byteLength)
    case 5122: return source instanceof Int16Array ? source : new Int16Array(buffer, byteOffset, byteLength / 2)
    case 5123: return source instanceof Uint16Array ? source : new Uint16Array(buffer, byteOffset, byteLength / 2)
    case 5124: return source instanceof Int32Array ? source : new Int32Array(buffer, byteOffset, byteLength / 4)
    case 5125: return source instanceof Uint32Array ? source : new Uint32Array(buffer, byteOffset, byteLength / 4)
    case 5126: return source instanceof Float32Array ? source : new Float32Array(buffer, byteOffset, byteLength / 4)
    default: throw new Error(`Unsupported S3M component datatype: ${attribute.componentDatatype}`)
  }
}

export function decodeS3MAttribute(vertexPackage, name) {
  const index = vertexPackage?.attrLocation?.[name]
  if (!Number.isInteger(index)) return null
  const attribute = vertexPackage.vertexAttributes[index]
  const source = attributeArray(attribute)
  if (!source) return null
  const itemSize = Number(attribute.componentsPerAttribute)

  if (name === 'aPosition' && (Number(vertexPackage.compressOptions) & SVC_VERTEX) === SVC_VERTEX) {
    const constant = Number(vertexPackage.vertCompressConstant)
    const minimum = vertexPackage.minVerticesValue || {}
    const decoded = new Float32Array((source.length / itemSize) * 3)
    for (let input = 0, output = 0; input < source.length; input += itemSize, output += 3) {
      decoded[output] = Number(minimum.x || 0) + source[input] * constant
      decoded[output + 1] = Number(minimum.y || 0) + source[input + 1] * constant
      decoded[output + 2] = Number(minimum.z || 0) + source[input + 2] * constant
    }
    return new THREE.BufferAttribute(decoded, 3, false)
  }

  if (name === 'aTexCoord0' && (Number(vertexPackage.compressOptions) & SVC_TEXTURE_COORD) === SVC_TEXTURE_COORD) {
    const rawConstant = vertexPackage.texCoordCompressConstant?.[0]
    const constantX = Number(rawConstant?.x ?? rawConstant ?? 1)
    const constantY = Number(rawConstant?.y ?? rawConstant ?? 1)
    const minimum = vertexPackage.minTexCoordValue?.[0] || {}
    const decoded = new Float32Array((source.length / itemSize) * itemSize)
    for (let input = 0; input < source.length; input += itemSize) {
      decoded[input] = Number(minimum.x || 0) + source[input] * constantX
      decoded[input + 1] = Number(minimum.y || 0) + source[input + 1] * constantY
      for (let component = 2; component < itemSize; component += 1) {
        decoded[input + component] = source[input + component]
      }
    }
    return new THREE.BufferAttribute(decoded, itemSize, false)
  }

  return new THREE.BufferAttribute(source, itemSize, Boolean(attribute.normalize))
}

function indexAttribute(indexPackage) {
  const source = indexPackage?.indicesTypedArray
  if (!source) return null
  if (source instanceof Uint16Array || source instanceof Uint32Array) {
    return new THREE.BufferAttribute(source, 1)
  }
  const uint32 = Number(indexPackage.indexType) === 1 || Number(indexPackage.indexType) === 3
  const array = uint32
    ? new Uint32Array(source.buffer, source.byteOffset, source.byteLength / 4)
    : new Uint16Array(source.buffer, source.byteOffset, Math.floor(source.byteLength / 2))
  return new THREE.BufferAttribute(array, 1)
}

function dxtLevelSize(format, width, height) {
  const blockBytes = format === THREE.RGB_S3TC_DXT1_Format || format === THREE.RGBA_S3TC_DXT1_Format ? 8 : 16
  return Math.max(1, Math.ceil(width / 4)) * Math.max(1, Math.ceil(height / 4)) * blockBytes
}

export function splitDXTMipmaps(textureInfo) {
  const format = Number(textureInfo?.internalFormat)
  if (!DXT_FORMATS.has(format)) {
    throw new Error(`Unsupported S3M DXT format: ${format}`)
  }
  const payload = textureInfo?.arrayBufferView
  if (!payload?.byteLength) throw new Error('S3M DXT texture payload is empty')
  const mipmaps = []
  let offset = 0
  let width = Number(textureInfo.width)
  let height = Number(textureInfo.height)
  while (offset < payload.byteLength) {
    const size = dxtLevelSize(format, width, height)
    if (offset + size > payload.byteLength) break
    mipmaps.push({ data: payload.subarray(offset, offset + size), width, height })
    offset += size
    width = Math.max(1, width >> 1)
    height = Math.max(1, height >> 1)
  }
  if (!mipmaps.length || offset !== payload.byteLength) {
    throw new Error(`Invalid S3M DXT mip chain: consumed=${offset} payload=${payload.byteLength}`)
  }
  return { format, mipmaps }
}

function createTexture(textureInfo, resources) {
  const { format, mipmaps } = splitDXTMipmaps(textureInfo)
  const texture = new THREE.CompressedTexture(
    mipmaps,
    Number(textureInfo.width),
    Number(textureInfo.height),
    format,
    THREE.UnsignedByteType
  )
  texture.colorSpace = THREE.SRGBColorSpace
  texture.minFilter = mipmaps.length > 1 ? THREE.LinearMipmapLinearFilter : THREE.LinearFilter
  texture.magFilter = THREE.LinearFilter
  texture.flipY = false
  texture.needsUpdate = true
  resources.textures.add(texture)
  return texture
}

function materialEntries(content) {
  const entries = content?.materials?.material || content?.materials?.materials || []
  return Array.isArray(entries) ? entries : []
}

function createMaterials(content, resources) {
  const textures = new Map()
  for (const [code, textureInfo] of Object.entries(content.texturePackage || {})) {
    if (textureInfo?.arrayBufferView?.byteLength) {
      textures.set(String(code), createTexture(textureInfo, resources))
    }
  }
  const materials = new Map()
  for (const entry of materialEntries(content)) {
    const source = entry?.material || entry
    const code = String(source?.id ?? source?.name ?? '')
    const textureStates = source?.textureunitstates || source?.textureStates || []
    const state = textureStates[0]?.textureunitstate || textureStates[0]?.textureUnitState || textureStates[0]
    const textureCode = String(state?.id ?? state?.textureName ?? '')
    const texture = textures.get(textureCode) || null
    if (texture) {
      const addressMode = state?.addressmode || state?.uAddressMode || {}
      texture.wrapS = Number(addressMode.u) === 0 ? THREE.RepeatWrapping : THREE.ClampToEdgeWrapping
      texture.wrapT = Number(addressMode.v) === 0 ? THREE.RepeatWrapping : THREE.ClampToEdgeWrapping
    }
    const diffuse = source?.diffuse || { r: 1, g: 1, b: 1, a: 1 }
    const opacity = Number(diffuse.a ?? 1)
    const material = new THREE.MeshBasicMaterial({
      color: new THREE.Color(Number(diffuse.r ?? 1), Number(diffuse.g ?? 1), Number(diffuse.b ?? 1)),
      opacity,
      transparent: opacity < 1,
      map: texture,
      side: THREE.DoubleSide
    })
    resources.materials.add(material)
    materials.set(code, material)
  }
  return materials
}

function materialForPrimitive(source, primitiveType, hasVertexColors, resources) {
  if (primitiveType === 2) {
    const material = new THREE.LineBasicMaterial({
      color: source?.color || 0xffffff,
      opacity: source?.opacity ?? 1,
      transparent: Boolean(source?.transparent),
      vertexColors: hasVertexColors
    })
    resources.materials.add(material)
    return material
  }
  if (primitiveType === 1) {
    const material = new THREE.PointsMaterial({
      color: source?.color || 0xffffff,
      opacity: source?.opacity ?? 1,
      transparent: Boolean(source?.transparent),
      vertexColors: hasVertexColors,
      size: 1,
      sizeAttenuation: false
    })
    resources.materials.add(material)
    return material
  }
  if (!source) {
    const material = new THREE.MeshBasicMaterial({
      color: 0xffffff,
      side: THREE.DoubleSide,
      vertexColors: hasVertexColors
    })
    resources.materials.add(material)
    return material
  }
  if (Boolean(source.vertexColors) === hasVertexColors) return source
  const material = source.clone()
  material.vertexColors = hasVertexColors
  resources.materials.add(material)
  return material
}

function pageBoundingSphere(group) {
  group.updateMatrixWorld(true)
  const box = new THREE.Box3().setFromObject(group)
  return box.isEmpty() ? null : box.getBoundingSphere(new THREE.Sphere())
}

function createPageLOD(content, pageNode, materialTable, resources, tileURL, stats) {
  const group = new THREE.Group()
  group.visible = false
  for (const geode of pageNode?.geodes || []) {
    const matrix = new THREE.Matrix4().fromArray(geode.matrix || new THREE.Matrix4().toArray())
    for (const skeletonName of geode?.skeletonNames || []) {
      const packageEntry = content.geoPackage?.[skeletonName]
      if (!packageEntry) continue
      const vertexPackage = packageEntry.vertexPackage
      const position = decodeS3MAttribute(vertexPackage, 'aPosition')
      const uv = decodeS3MAttribute(vertexPackage, 'aTexCoord0')
      const color = decodeS3MAttribute(vertexPackage, 'aColor')
      if (!position) continue
      for (const packageIndex of packageEntry.arrIndexPackage || []) {
        const geometry = new THREE.BufferGeometry()
        geometry.setAttribute('position', position.clone())
        if (uv) geometry.setAttribute('uv', uv.clone())
        if (color) geometry.setAttribute('color', color.clone())
        const indices = indexAttribute(packageIndex)
        if (indices) geometry.setIndex(indices)
        geometry.computeBoundingSphere()
        resources.geometries.add(geometry)
        const primitiveType = Number(packageIndex.primitiveType)
        const sourceMaterial = materialTable.get(String(packageIndex.materialCode))
        const material = materialForPrimitive(sourceMaterial, primitiveType, Boolean(color), resources)
        const object = primitiveType === 2
          ? new THREE.LineSegments(geometry, material)
          : primitiveType === 1
            ? new THREE.Points(geometry, material)
            : new THREE.Mesh(geometry, material)
        object.matrix.copy(matrix)
        object.matrixAutoUpdate = false
        group.add(object)
        stats.meshes += 1
        stats.vertices += Number(vertexPackage.verticesCount || position.count)
        if (primitiveType === 4) {
          stats.triangles += Math.floor(Number(packageIndex.indicesCount || indices?.count || 0) / 3)
        }
      }
    }
  }
  const childReference = String(pageNode?.childTile || '').trim()
  return {
    group,
    sphere: pageBoundingSphere(group),
    rangeMode: Number(pageNode?.rangeMode || 0),
    rangeList: Number(pageNode?.rangeList || 0),
    childURL: childReference ? resolveS3MResourceURL(childReference, tileURL) : '',
    child: null
  }
}

export function createS3MThreeContent(content, tileURL) {
  const resources = {
    geometries: new Set(),
    materials: new Set(),
    textures: new Set()
  }
  const stats = { meshes: 0, vertices: 0, triangles: 0 }
  const materialTable = createMaterials(content, resources)
  const pageLods = (content?.groupNode?.pageLods || []).map(page => (
    createPageLOD(content, page, materialTable, resources, tileURL, stats)
  ))
  return { pageLods, resources, stats }
}

export function s3mRootTilesBoundingBox(rootTiles) {
  const box = new THREE.Box3()
  for (const tile of Array.isArray(rootTiles) ? rootTiles : []) {
    if (!tile?.group) continue
    tile.group.updateMatrixWorld(true)
    box.expandByObject(tile.group)
  }
  return box.isEmpty() ? null : box
}

export function s3mRootTilesBoundingSphere(rootTiles) {
  const box = s3mRootTilesBoundingBox(rootTiles)
  return box ? box.getBoundingSphere(new THREE.Sphere()) : null
}

export function disposeS3MThreeResources(resources) {
  for (const geometry of resources?.geometries || []) geometry.dispose()
  for (const material of resources?.materials || []) material.dispose()
  for (const texture of resources?.textures || []) texture.dispose()
  resources?.geometries?.clear()
  resources?.materials?.clear()
  resources?.textures?.clear()
}
