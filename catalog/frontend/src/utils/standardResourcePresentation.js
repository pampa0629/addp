const RESOURCES = Object.freeze({
 glossary: ['glossaries', 'standard.glossary.read'], element: ['elements', 'standard.element.read'],
 code_set: ['code-sets', 'standard.code_set.read'], document: ['documents', 'standard.document.read'], metric: ['metrics', 'standard.metric.read']
})
export function standardResourcePresentation(kind, identity, revisionId = '') {
 const sourceType = kind === 'standard_document' ? 'document' : kind
 const definition = RESOURCES[sourceType]
 const id = String(identity || '')
 if (!definition || !/^[1-9]\d*$/.test(id)) return null
 const path = `/standard/${definition[0]}/${encodeURIComponent(id)}`
 return { sourceType, path, permission: definition[1], detailPath: path + (revisionId ? `?revision_id=${encodeURIComponent(revisionId)}` : '') }
}
export function standardDefinitionRevision(aggregate) {
 const revision = aggregate?.current_revision || aggregate?.latest_revision
 return revision ? { revision, isCurrent: Boolean(aggregate.current_revision) } : null
}
export async function loadStandardDefinition(state, { source, hasPermission, read }) {
 const version = ++state.version
 state.text = ''; state.revision = null
 const config = source?.source_module === 'standard' ? standardResourcePresentation(source.source_type, source.source_identity) : null
 state.config = config
 if (!config) { state.status = 'unavailable'; return }
 if (!hasPermission(config.permission)) { state.status = 'forbidden'; return }
 if (source.source_status !== 'active') { state.status = 'missing'; return }
 state.status = 'loading'
 try {
  const aggregate = await read(config.path)
  if (version !== state.version) return
  const display = standardDefinitionRevision(aggregate)
  state.revision = display
  state.text = display ? (display.revision.definition || display.revision.description || '') : ''
  state.status = 'ready'
 } catch (error) {
  if (version !== state.version) return
  state.status = error?.response?.status === 403 ? 'forbidden' : error?.response?.status === 404 ? 'missing' : 'unavailable'
 }
}
