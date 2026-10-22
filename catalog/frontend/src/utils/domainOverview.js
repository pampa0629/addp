import { parseEntryListRoute } from './entryRouteState'

export const DOMAIN_REFERENCE_GROUPS = [
 { key: 'catalog', permission: 'catalog.entry.read', path: '/catalog/domain_standard_references' },
 { key: 'modelEntities', permission: 'model.entity.read', path: '/model/entities/standard_references' },
 { key: 'modelTables', permission: 'model.logical_model.read', path: '/model/logical-tables/standard_references' },
 { key: 'modelMetrics', permission: 'model.metric_implementation.read', path: '/model/metric-implementations/standard_references' },
 { key: 'standardGlossaries', permission: 'standard.glossary.read', path: '/standard/glossaries/standard_references' },
 { key: 'standardElements', permission: 'standard.element.read', path: '/standard/elements/standard_references' },
 { key: 'standardMetrics', permission: 'standard.metric.read', path: '/standard/metrics/standard_references' }
]
export function domainOverviewState(query, inventoryAllowed) {
 const parsed = parseEntryListRoute({ primary_domain_id: query.domain_id, view: query.view })
 return { domainId: parsed.primary_domain_id, view: parsed.view === 'inventory' && inventoryAllowed ? 'inventory' : 'governance' }
}
export function domainOverviewQuery(state) {
 return { ...(state.domainId ? { domain_id: state.domainId } : {}), ...(state.view === 'inventory' ? { view: 'inventory' } : {}) }
}
export function referenceOwnership(reference, domainId) {
 if (!reference?.found) return 'unavailable'
 if (reference.owner_domain_id) return String(reference.owner_domain_id) === domainId ? 'local' : 'external'
 return ['platform', 'tenant_common'].includes(reference.scope_type) ? 'public' : 'unavailable'
}

export async function loadDomainReferenceGroup(group, { domainId, view, hasPermission, read, resolve }) {
 const version = ++group.version
 group.items = []; group.total = 0
 if (!hasPermission(group.permission)) { group.status = 'forbidden'; return }
 if (!domainId) { group.status = 'idle'; return }
 group.status = 'loading'
 try {
  const result = await read(group.path, { domain_id: domainId, page: group.page, page_size: 5, ...(group.key === 'catalog' ? { view } : {}) })
  if (version !== group.version) return
  const rows = result.data || []
  const resolved = rows.length ? await resolve(rows.map(row => ({ source_module: 'standard', source_type: row.source_type, source_identity: row.source_identity }))) : { results: [] }
  if (version !== group.version) return
  const byKey = new Map((resolved.results || []).map(row => [`${row.source_type}:${row.source_identity}`, row]))
  group.items = rows.map(row => {
   const target = byKey.get(`${row.source_type}:${row.source_identity}`)
   return { ...row, target, ownership: referenceOwnership(target, domainId) }
  }).filter(row => row.ownership !== 'local')
  // The owner total paginates all explicit references, not an external-target total.
  group.total = Number(result.total) || 0
  group.status = 'ready'
 } catch (error) {
  if (version !== group.version) return
  group.status = error?.response?.status === 403 ? 'forbidden' : 'unavailable'
 }
}
