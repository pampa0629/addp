const GLB_MAGIC = 0x46546c67
const GLB_VERSION_2 = 2
const JSON_CHUNK_TYPE = 0x4e4f534a
const MATERIAL_EXTENSION_UNLIT = 'KHR_materials_unlit'

export function patchGLBMissingMaterialExtensions(data) {
  if (!(data instanceof ArrayBuffer) || data.byteLength < 20) return data
  const view = new DataView(data)
  if (view.getUint32(0, true) !== GLB_MAGIC || view.getUint32(4, true) !== GLB_VERSION_2) return data

  const jsonChunkLength = view.getUint32(12, true)
  const jsonChunkType = view.getUint32(16, true)
  if (jsonChunkType !== JSON_CHUNK_TYPE || 20 + jsonChunkLength > data.byteLength) return data

  let json
  try {
    const jsonText = new TextDecoder().decode(new Uint8Array(data, 20, jsonChunkLength)).trim()
    json = JSON.parse(jsonText)
  } catch {
    return data
  }

  if (!needsUnlitExtensionDeclaration(json)) return data

  const extensionsUsed = Array.isArray(json.extensionsUsed) ? [...json.extensionsUsed] : []
  extensionsUsed.push(MATERIAL_EXTENSION_UNLIT)
  json.extensionsUsed = extensionsUsed

  const encoder = new TextEncoder()
  const jsonBytes = encoder.encode(JSON.stringify(json))
  const paddedJSONLength = align4(jsonBytes.byteLength)
  const restOffset = 20 + jsonChunkLength
  const restBytes = new Uint8Array(data, restOffset)
  const output = new ArrayBuffer(20 + paddedJSONLength + restBytes.byteLength)
  const outputView = new DataView(output)
  const outputBytes = new Uint8Array(output)

  outputView.setUint32(0, GLB_MAGIC, true)
  outputView.setUint32(4, GLB_VERSION_2, true)
  outputView.setUint32(8, output.byteLength, true)
  outputView.setUint32(12, paddedJSONLength, true)
  outputView.setUint32(16, JSON_CHUNK_TYPE, true)
  outputBytes.set(jsonBytes, 20)
  outputBytes.fill(0x20, 20 + jsonBytes.byteLength, 20 + paddedJSONLength)
  outputBytes.set(restBytes, 20 + paddedJSONLength)
  return output
}

function needsUnlitExtensionDeclaration(json) {
  if (!json || !Array.isArray(json.materials)) return false
  if (Array.isArray(json.extensionsUsed) && json.extensionsUsed.includes(MATERIAL_EXTENSION_UNLIT)) return false
  return json.materials.some((material) => Boolean(material?.extensions?.[MATERIAL_EXTENSION_UNLIT]))
}

function align4(value) {
  return Math.ceil(value / 4) * 4
}
