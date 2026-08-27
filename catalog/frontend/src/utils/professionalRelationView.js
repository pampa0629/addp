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
