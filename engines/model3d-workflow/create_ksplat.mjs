import fs from 'node:fs/promises'
import path from 'node:path'
import * as THREE from 'three'
import * as GaussianSplats3D from '@mkkellogg/gaussian-splats-3d'

globalThis.window = globalThis.window || {
  setTimeout: globalThis.setTimeout,
  clearTimeout: globalThis.clearTimeout
}

function usage() {
  return [
    'Usage:',
    'node create_ksplat.mjs <source> <target> <source_format> [compression_level] [alpha_threshold] [sh_degree] [section_size] [scene_center] [block_size] [bucket_size]'
  ].join('\n')
}

function intArg(value, fallback, min, max) {
  const parsed = Number.parseInt(String(value ?? ''), 10)
  if (!Number.isFinite(parsed)) return fallback
  return Math.max(min, Math.min(max, parsed))
}

function floatArg(value, fallback, min, max) {
  const parsed = Number.parseFloat(String(value ?? ''))
  if (!Number.isFinite(parsed)) return fallback
  return Math.max(min, Math.min(max, parsed))
}

function parseSceneCenter(value) {
  const text = String(value ?? '').trim()
  if (!text) return [0, 0, 0]
  const parts = text.split(',').map((part) => Number.parseFloat(part.trim()))
  if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) {
    throw new Error(`Invalid scene_center: ${text}`)
  }
  return parts
}

const [
  sourcePath,
  targetPath,
  rawSourceFormat,
  rawCompressionLevel,
  rawAlphaThreshold,
  rawSHDegree,
  rawSectionSize,
  rawSceneCenter,
  rawBlockSize,
  rawBucketSize
] = process.argv.slice(2)
if (!sourcePath || !targetPath || !rawSourceFormat) {
  console.error(usage())
  process.exit(2)
}

const sourceFormat = rawSourceFormat.trim().toLowerCase()
const compressionLevel = intArg(rawCompressionLevel, 1, 0, 2)
const alphaThreshold = intArg(rawAlphaThreshold, 1, 0, 255)
const sphericalHarmonicsDegree = intArg(rawSHDegree, 0, 0, 2)
const sectionSize = intArg(rawSectionSize, 262144, 1, Number.MAX_SAFE_INTEGER)
let sceneCenterValues
try {
  sceneCenterValues = parseSceneCenter(rawSceneCenter)
} catch (error) {
  console.error(error.message)
  process.exit(2)
}
const sceneCenter = new THREE.Vector3(sceneCenterValues[0], sceneCenterValues[1], sceneCenterValues[2])
const blockSize = floatArg(rawBlockSize, 5.0, 0.000001, Number.MAX_VALUE)
const bucketSize = intArg(rawBucketSize, 256, 1, Number.MAX_SAFE_INTEGER)

const source = await fs.readFile(sourcePath)
const sourceBuffer = source.buffer.slice(source.byteOffset, source.byteOffset + source.byteLength)

let splatBuffer
if (sourceFormat === 'ply') {
  splatBuffer = await GaussianSplats3D.PlyLoader.loadFromFileData(
    sourceBuffer,
    alphaThreshold,
    compressionLevel,
    true,
    sphericalHarmonicsDegree,
    sectionSize,
    sceneCenter,
    blockSize,
    bucketSize
  )
} else if (sourceFormat === 'splat') {
  splatBuffer = await GaussianSplats3D.SplatLoader.loadFromFileData(
    sourceBuffer,
    alphaThreshold,
    compressionLevel,
    true,
    sectionSize,
    sceneCenter,
    blockSize,
    bucketSize
  )
} else {
  console.error(`Unsupported source format: ${sourceFormat}`)
  process.exit(2)
}

await fs.mkdir(path.dirname(targetPath), { recursive: true })
await fs.writeFile(targetPath, Buffer.from(splatBuffer.bufferData))

const facts = {
  source_format: sourceFormat,
  target_format: 'ksplat',
  compression_level: compressionLevel,
  alpha_threshold: alphaThreshold,
  spherical_harmonics_degree: sphericalHarmonicsDegree,
  section_size: sectionSize,
  scene_center: sceneCenterValues,
  block_size: blockSize,
  bucket_size: bucketSize,
  size_bytes: splatBuffer.bufferData.byteLength
}
console.log(JSON.stringify(facts))
