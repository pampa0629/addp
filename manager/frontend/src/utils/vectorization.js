const storageEngineTypes = new Set(['minio', 's3', 'nfs', 'nas'])
const objectNodeTypes = new Set(['object', 'file'])
const rangeNodeTypes = new Set(['directory', 'bucket', 'prefix', 'dir'])

export const SUPPORTED_VECTOR_EXTENSIONS = [
  'txt',
  'md',
  'markdown',
  'csv',
  'json',
  'jsonl',
  'pdf',
  'doc',
  'docx',
  'ppt',
  'pptx',
  'xls',
  'xlsx',
  'jpg',
  'jpeg',
  'png',
  'gif',
  'bmp',
  'webp'
]

export const DEFAULT_VECTOR_MAX_FILE_SIZE_MB = 10

const vectorizableExtensions = new Set(SUPPORTED_VECTOR_EXTENSIONS)

const unsupportedSuffixes = [
  '.aux.xml',
  '.tfw',
  '.jgw',
  '.pgw',
  '.wld',
  '.ovr',
  '.prj',
  '.shx',
  '.qix',
  '.cpg'
]

export const isStorageEngineNode = (node) => {
  const engineType = String(node?.engineType || node?.engine_type || '').toLowerCase()
  return storageEngineTypes.has(engineType)
}

export const normalizedNodeType = (node) => String(node?.nodeType || node?.type || '').toLowerCase()

export const isObjectNode = (node) => objectNodeTypes.has(normalizedNodeType(node))

export const isVectorizableRangeNode = (node) => {
  return isStorageEngineNode(node) && rangeNodeTypes.has(normalizedNodeType(node))
}

export const vectorizationPath = (node, previewData = null) => {
  return String(
    previewData?.object?.path ||
      previewData?.object?.storage_ref ||
      previewData?.object?.storageRef ||
      node?.path ||
      node?.table ||
      node?.label ||
      node?.name ||
      ''
  ).trim()
}

export const vectorizationExtension = (node, previewData = null) => {
  const path = vectorizationPath(node, previewData).toLowerCase()
  const name = path.split('/').pop() || path
  const index = name.lastIndexOf('.')
  return index >= 0 ? name.slice(index + 1) : ''
}

export const isVectorizableObjectNode = (node, previewData = null) => {
  if (!isStorageEngineNode(node) || !isObjectNode(node)) {
    return false
  }
  const path = vectorizationPath(node, previewData).toLowerCase()
  if (!path) {
    return false
  }
  if (unsupportedSuffixes.some((suffix) => path.endsWith(suffix))) {
    return false
  }
  return vectorizableExtensions.has(vectorizationExtension(node, previewData))
}

export const canShowVectorizeAction = (node, embeddingState = null, previewData = null) => {
  if (isVectorizableRangeNode(node)) {
    return true
  }
  if (!isVectorizableObjectNode(node, previewData)) {
    return false
  }
  const status = embeddingState?.embedding?.status
  return status !== 'ready' && status !== 'unsupported'
}

export const isEmbeddingReady = (embeddingState = null) => {
  return embeddingState?.embedding?.status === 'ready'
}
