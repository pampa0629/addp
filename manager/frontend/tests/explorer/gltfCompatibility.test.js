import { describe, expect, it } from 'vitest'
import { patchGLBMissingMaterialExtensions } from '../../src/utils/gltfCompatibility'

describe('patchGLBMissingMaterialExtensions', () => {
  it('adds missing KHR_materials_unlit declaration used by materials', () => {
    const glb = makeGLB({
      asset: { version: '2.0' },
      materials: [
        {
          extensions: { KHR_materials_unlit: {} },
          pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] }
        }
      ]
    })

    const patched = patchGLBMissingMaterialExtensions(glb)
    const json = readGLBJSON(patched)
    expect(json.extensionsUsed).toContain('KHR_materials_unlit')
    expect(new DataView(patched).getUint32(8, true)).toBe(patched.byteLength)
  })

  it('keeps GLB unchanged when unlit material extension is not used', () => {
    const glb = makeGLB({
      asset: { version: '2.0' },
      materials: [
        {
          pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] }
        }
      ]
    })

    expect(patchGLBMissingMaterialExtensions(glb)).toBe(glb)
  })
})

function makeGLB(json) {
  const jsonBytes = new TextEncoder().encode(JSON.stringify(json))
  const jsonLength = align4(jsonBytes.byteLength)
  const output = new ArrayBuffer(20 + jsonLength)
  const view = new DataView(output)
  const bytes = new Uint8Array(output)
  view.setUint32(0, 0x46546c67, true)
  view.setUint32(4, 2, true)
  view.setUint32(8, output.byteLength, true)
  view.setUint32(12, jsonLength, true)
  view.setUint32(16, 0x4e4f534a, true)
  bytes.set(jsonBytes, 20)
  bytes.fill(0x20, 20 + jsonBytes.byteLength, 20 + jsonLength)
  return output
}

function readGLBJSON(glb) {
  const view = new DataView(glb)
  const jsonLength = view.getUint32(12, true)
  const jsonText = new TextDecoder().decode(new Uint8Array(glb, 20, jsonLength)).trim()
  return JSON.parse(jsonText)
}

function align4(value) {
  return Math.ceil(value / 4) * 4
}
