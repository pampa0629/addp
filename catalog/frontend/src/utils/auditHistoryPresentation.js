const eventKeys = Object.freeze({
  source_discovered: 'sourceDiscovered',
  'catalog.entry.updated': 'entryUpdated',
  'catalog.entry.curation_withdrawn': 'curationWithdrawn',
  'catalog.entry.certified': 'entryCertified',
  'catalog.entry.certification_withdrawn': 'certificationWithdrawn',
  'catalog.entry.deprecated': 'entryDeprecated',
  'catalog.entry.deprecation_updated': 'deprecationUpdated',
  'catalog.entry.deprecation_withdrawn': 'deprecationWithdrawn',
  'catalog.entry.responsibilities_transferred': 'responsibilitiesTransferred',
  'catalog.entry.batch_governance_applied': 'batchGovernanceApplied',
  'catalog.entry.merged': 'entryMerged',
  'catalog.source.rebound': 'sourceRebound',
  'catalog.responsibility.reference_state_changed': 'responsibilityStateChanged',
  'catalog.sharing_decision.created': 'dataUseApproved',
  'catalog.standard_mapping.proposed': 'mappingProposed',
  'catalog.standard_mapping.updated': 'mappingUpdated',
  'catalog.standard_mapping.deleted': 'mappingDeleted',
  'catalog.standard_mapping.approved': 'mappingApproved',
  'catalog.standard_mapping.rejected': 'mappingRejected',
  'catalog.standard_mapping.withdrawn': 'mappingWithdrawn'
})
const roles = ['accountable_department', 'business_owner', 'data_steward', 'technical_owner']
const governanceStates = ['discovered', 'curated', 'certified', 'deprecated']
const visibilityStates = ['inventory', 'department', 'tenant']
const reviewStates = ['proposed', 'approved', 'rejected', 'withdrawn']
const sourceModules = ['meta', 'model', 'standard', 'service', 'develop', 'workbench']

export function auditEventTitle(event, t) {
  return t(`catalog.history.events.${Object.hasOwn(eventKeys, event?.event_type) ? eventKeys[event.event_type] : 'other'}`)
}

function countText(value) {
  if (typeof value === 'number') return Number.isSafeInteger(value) && value >= 0 ? String(value) : ''
  return typeof value === 'string' && /^(0|[1-9]\d*)$/.test(value) ? value : ''
}

export function auditEventSummary(event, t) {
  if (!Object.hasOwn(eventKeys, event?.event_type)) return []
  const details = event.details && typeof event.details === 'object' && !Array.isArray(event.details) ? event.details : {}
  const rows = []
  const add = (key, value) => { if (value !== '') rows.push({ label: t(key), value }) }
  const label = (prefix, value, allowed) => allowed.includes(value) ? t(`${prefix}.${value}`) : ''
  const transition = (previous, current) => previous && previous !== current ? `${previous} → ${current}` : current

  for (const [field, key] of [['reason', 'catalog.governance.reason'], ['opinion', 'catalog.history.fields.opinion'], ['evidence', 'catalog.rebind.evidence']]) {
    if (typeof details[field] === 'string') add(key, details[field])
  }
  const currentState = label('catalog.status.governance', details.governance_status, governanceStates)
  if (currentState) add('catalog.entries.governanceStatus', transition(label('catalog.status.governance', details.previous_governance_status, governanceStates), currentState))
  add('catalog.entries.visibility', label('catalog.status.visibility', details.visibility, visibilityStates))
  add('catalog.mapping.status', label('catalog.mapping.states', details.review_status, reviewStates))
  add('catalog.entry.sourceModule', label('catalog.history.modules', details.source_module, sourceModules))

  const version = countText(details.version) || countText(details.entry_version)
  if (version) add(`catalog.history.fields.${event.event_type.startsWith('catalog.standard_mapping.') ? 'mappingVersion' : 'entryVersion'}`, transition(countText(details.previous_version), version))
  for (const [field, key] of [['domain_count', 'domainCount'], ['glossary_count', 'glossaryCount'], ['responsibility_count', 'responsibilityCount']]) {
    add(`catalog.history.fields.${key}`, countText(details[field]))
  }
  const responsibilityRows = Array.isArray(details.responsibilities) ? details.responsibilities : Array.isArray(details.changes) ? details.changes : []
  const recordedRoles = [...new Set(responsibilityRows.map(row => row?.role).filter(role => roles.includes(role)))]
  if (recordedRoles.length) add('catalog.edit.responsibilityRole', recordedRoles.map(role => t(`catalog.edit.role.${role}`)).join(t('catalog.edit.listSeparator')))
  if (Array.isArray(details.changes)) add('catalog.history.fields.responsibilityChanges', String(details.changes.length))
  if (event.event_type === 'catalog.responsibility.reference_state_changed' && Array.isArray(details.changes)) {
    details.changes.forEach((change, index) => {
      if (!change || typeof change !== 'object' || Array.isArray(change)) return
      const role = label('catalog.edit.role', change.role, roles) || t('catalog.edit.responsibilityRole')
      const state = value => label('catalog.status.responsibility', value, ['active', 'needs_transfer']) || t('catalog.history.responsibilityChange.unknownState')
      const reason = label('catalog.governanceTasks.reasonValue', change.reason, ['subject_not_found', 'subject_not_referenceable']) ||
        t(`catalog.history.responsibilityChange.${change.reason === 'reference_restored' ? 'referenceRestored' : 'unknownReason'}`)
      rows.push({
        label: t('catalog.history.responsibilityChange.label', { role, index: index + 1 }),
        value: t('catalog.history.responsibilityChange.summary', { previous: state(change.previous_status), current: state(change.status), reason })
      })
    })
  }
  if (Array.isArray(details.approved_standard_mappings)) add('catalog.history.fields.certifiedMappingCount', String(details.approved_standard_mappings.length))
  if (details.operation === 'assign_primary_domain') add('catalog.entries.batchGovernance.operation', t('catalog.entries.batchGovernance.assignPrimaryDomain'))
  if (details.operation === 'assign_accountable_department') add('catalog.entries.batchGovernance.operation', t('catalog.entries.batchGovernance.assignAccountableDepartment'))
  return rows
}
