const POSITIVE_DECIMAL_ID = /^[1-9][0-9]*$/

const RELATION_SOURCES = {
  'model:entity': {
    path: id => `/model/entities/${encodeURIComponent(id)}/relations`,
    permissions: ['model.entity.read', 'model.entity_relation.read']
  },
  'model:logical_table': {
    path: id => `/model/logical-tables/${encodeURIComponent(id)}/relations`,
    permissions: ['model.logical_model.read']
  },
  'standard:metric': {
    path: id => `/standard/metrics/${encodeURIComponent(id)}/relations`,
    permissions: ['standard.metric.read']
  }
}

export function resolveProfessionalRelationSubject(entry) {
  const source = entry?.source
  if (entry?.entry_status !== 'active' || source?.source_status !== 'active') return null
  const sourceIdentity = String(source?.source_identity || '')
  const config = RELATION_SOURCES[`${source?.source_module}:${source?.source_type}`]
  if (!config || !POSITIVE_DECIMAL_ID.test(sourceIdentity)) return null
  return {
    key: `${source.source_module}:${source.source_type}:${sourceIdentity}`,
    owner_module: source.source_module,
    resource_type: source.source_type,
    resource_id: sourceIdentity,
    path: config.path(sourceIdentity),
    permissions: [...config.permissions]
  }
}

export function normalizeProfessionalRelations(payload) {
  return {
    schema_version: payload?.schema_version || '',
    subject: payload?.subject || null,
    nodes: Array.isArray(payload?.nodes) ? payload.nodes : [],
    edges: Array.isArray(payload?.edges) ? payload.edges : [],
    truncated: Boolean(payload?.truncated)
  }
}

export function professionalRelationFailureState(error) {
  if (error?.response?.status === 403) return 'forbidden'
  if (error?.response?.status === 404) return 'subject_missing'
  return 'unavailable'
}

export function professionalResourceKey(resource) {
  return `${resource?.owner_module || ''}:${resource?.resource_type || ''}:${resource?.resource_id || ''}`
}

export function professionalNodesToSourceReferences(nodes, limit = 200) {
  const supported = new Set(['model:entity', 'model:logical_table', 'standard:metric'])
  const seen = new Set()
  const references = []
  for (const node of Array.isArray(nodes) ? nodes : []) {
    const ownerModule = String(node?.owner_module || '')
    const resourceType = String(node?.resource_type || '')
    const resourceID = String(node?.resource_id || '')
    const key = `${ownerModule}:${resourceType}:${resourceID}`
    if (!supported.has(`${ownerModule}:${resourceType}`) || !POSITIVE_DECIMAL_ID.test(resourceID) || seen.has(key)) continue
    seen.add(key)
    references.push({ source_module: ownerModule, source_type: resourceType, source_identity: resourceID })
    if (references.length >= limit) break
  }
  return references
}

export function sourceEntryResolutionMap(results) {
  const mapping = new Map()
  for (const result of Array.isArray(results) ? results : []) {
    if (!result?.found || !result.entry?.id) continue
    mapping.set(professionalResourceKey({
      owner_module: result.source_module,
      resource_type: result.source_type,
      resource_id: result.source_identity
    }), result.entry)
  }
  return mapping
}
