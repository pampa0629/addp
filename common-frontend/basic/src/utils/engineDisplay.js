export const parseEngineCapabilities = (capabilities) => {
  if (!capabilities) return {}
  if (typeof capabilities === 'object') return capabilities
  try {
    return JSON.parse(capabilities)
  } catch {
    return {}
  }
}

export const getEngineFamily = (engine = {}) => {
  const caps = parseEngineCapabilities(engine.capabilities)
  return caps.engine_family || engine.engine_family || ''
}

export const getEngineIconName = (engine = {}) => {
  const family = getEngineFamily(engine)
  const familyIcons = {
    tabular: 'Database',
    object: 'FolderOpen',
    file: 'FolderOpen',
    dynamic_schema: 'DocumentText',
    document: 'DocumentText',
    graph: 'Share',
    workflow: 'Grid',
    script: 'CodeBracket',
    compute: 'Grid'
  }
  if (familyIcons[family]) return familyIcons[family]

  const caps = parseEngineCapabilities(engine.capabilities)
  if (caps.compute?.workflow?.supported) return 'Grid'
  if (caps.compute?.script?.supported) return 'CodeBracket'
  if (caps.compute?.query?.supported) return 'Database'
  if (caps.storage?.catalog_model?.root_term === 'service') return 'FolderOpen'
  if (caps.storage?.catalog_model?.root_term === 'root') return 'FolderOpen'
  if (caps.storage?.catalog_model?.levels?.some(level => level.term === 'collection')) return 'DocumentText'
  if (caps.storage?.catalog_model?.levels?.some(level => level.term === 'label')) return 'Share'
  if (caps.storage) return 'Database'

  return fallbackEngineTypeIcon(engine.engine_type || engine.engineType || engine.type)
}

export const fallbackEngineTypeIcon = (engineType = '') => {
  const normalized = String(engineType).toLowerCase()
  if (['minio', 's3', 'nfs', 'nas'].includes(normalized)) return 'FolderOpen'
  if (normalized === 'mongodb') return 'DocumentText'
  if (normalized === 'neo4j') return 'Share'
  if (normalized.includes('workflow') || normalized === 'spark') return 'Grid'
  return 'Database'
}

export const getEngineFamilyLabelKey = (family = '') => {
  const keys = {
    tabular: 'system.engine.capabilities.tabular',
    object: 'system.engine.capabilities.objectStorage',
    file: 'system.engine.capabilities.file',
    dynamic_schema: 'system.engine.capabilities.dynamicSchema',
    document: 'system.engine.capabilities.document',
    graph: 'system.engine.capabilities.graphDb'
  }
  return keys[family] || ''
}
