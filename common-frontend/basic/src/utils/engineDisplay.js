export const getEngineFamily = (engine = {}) => {
  const summary = normalizeCapabilitiesView(engine.capabilities_view).summary
  const familyBadge = summary.find(badge => badge.id === 'engine_family')
  return familyFromCapabilityValue(familyBadge?.value || familyBadge?.value_key) || engine.engine_family || ''
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

  const view = normalizeCapabilitiesView(engine.capabilities_view)
  if (view.sections.some(section => section.id === 'compute')) {
    if (hasCapability(view, 'workflow')) return 'Grid'
    if (hasCapability(view, 'script')) return 'CodeBracket'
    if (hasCapability(view, 'query')) return 'Database'
  }
  if (view.sections.some(section => section.id === 'storage')) {
    if (hasCatalogTerm(view, ['service', 'root', 'bucket', 'prefix'])) return 'FolderOpen'
    if (hasCatalogTerm(view, ['collection'])) return 'DocumentText'
    if (hasCatalogTerm(view, ['label'])) return 'Share'
    return 'Database'
  }

  return fallbackEngineTypeIcon(engine.engine_type || engine.engineType || engine.type)
}

export const fallbackEngineTypeIcon = (engineType = '') => {
  const normalized = String(engineType).toLowerCase()
  if (['minio', 's3', 'nfs', 'nas'].includes(normalized)) return 'FolderOpen'
  if (normalized === 'mongodb') return 'DocumentText'
  if (normalized === 'neo4j') return 'Share'
  if (normalized.includes('workflow')) return 'Grid'
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

const normalizeCapabilitiesView = (view) => {
  if (!view || typeof view !== 'object') {
    return { summary: [], sections: [] }
  }
  return {
    summary: Array.isArray(view.summary) ? view.summary : [],
    sections: Array.isArray(view.sections) ? view.sections : []
  }
}

const familyFromCapabilityValue = (value = '') => {
  const text = String(value)
  const marker = 'system.engine.capabilityView.engineFamily.'
  if (text.startsWith(marker)) {
    const key = text.slice(marker.length)
    return key.replace(/[A-Z]/g, char => `_${char.toLowerCase()}`)
  }
  return text
}

const hasCapability = (view, id) => {
  return view.summary.some(badge => badge.id === id) ||
    view.sections.some(section => (section.items || []).some(item => item.id === id))
}

const hasCatalogTerm = (view, terms) => {
  return view.sections.some(section => {
    return (section.path || []).some(node => terms.includes(node.value || node.id))
  })
}
