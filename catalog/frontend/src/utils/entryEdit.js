const RESPONSIBILITY_SUBJECT_TYPES = Object.freeze({
  accountable_department: 'department',
  business_owner: 'user',
  data_steward: 'user',
  technical_owner: 'user'
})
const REQUIRED_CURATION_ROLES = ['accountable_department', 'business_owner', 'data_steward']

export function buildEntryEditForm(entry) {
  const semanticLinks = Array.isArray(entry?.semantic_links) ? entry.semantic_links : []
  const ownerManagedSemantics = ['model', 'standard'].includes(entry?.source?.source_module)
  return {
    version: Number(entry?.version || 0),
    businessName: entry?.business_name || '',
    businessDescription: entry?.business_description || '',
    governanceStatus: entry?.governance_status || 'discovered',
    visibility: entry?.visibility || 'inventory',
    ownerManagedSemantics,
    ownerModule: entry?.source?.source_module || '',
    ownerPrimaryDomainId: ownerManagedSemantics
      ? String(entry?.source_resolution?.summary?.domain_id || entry?.source?.observed_snapshot?.domain_id || '')
      : '',
    ownerScopeType: ownerManagedSemantics
      ? String(entry?.source_resolution?.summary?.scope_type || entry?.source?.observed_snapshot?.scope_type || '')
      : '',
    domains: semanticLinks
      .filter(link => link.semantic_type === 'domain')
      .filter(link => !ownerManagedSemantics || link.relation_role === 'secondary')
      .map(link => ({ id: String(link.semantic_id), role: link.relation_role })),
    glossaryIDs: semanticLinks
      .filter(link => link.semantic_type === 'glossary')
      .map(link => String(link.semantic_id)),
    responsibilities: (Array.isArray(entry?.responsibilities) ? entry.responsibilities : [])
      .filter(item => item.status === 'active')
      .map(item => ({ role: item.role, subjectId: String(item.subject_id) }))
  }
}

export function withRequiredResponsibilityRows(form) {
  const responsibilities = [...form.responsibilities]
  for (const role of REQUIRED_CURATION_ROLES) {
    if (!responsibilities.some(item => item.role === role)) responsibilities.push({ role, subjectId: '' })
  }
  return { ...form, responsibilities }
}

export function requiredCurationGaps(form) {
  const gaps = []
  if (!String(form.businessName || '').trim()) gaps.push('businessName')
  if (!String(form.businessDescription || '').trim()) gaps.push('businessDescription')
  if (!hasEffectivePrimaryDomain(form)) gaps.push('primaryDomain')
  for (const [role, key] of [
    ['accountable_department', 'accountableDepartment'],
    ['business_owner', 'businessOwner'],
    ['data_steward', 'dataSteward']
  ]) {
    const assigned = form.responsibilities.filter(item => item.role === role && isCanonicalPositiveID(item.subjectId))
    if (role === 'data_steward' ? assigned.length < 1 : assigned.length !== 1) gaps.push(key)
  }
  return gaps
}

export function buildUpdatePayload(form) {
  const payload = {
    version: Number(form.version),
    business_name: nullableTrimmed(form.businessName),
    business_description: nullableTrimmed(form.businessDescription),
    governance_status: form.governanceStatus,
    visibility: form.visibility,
    domains: form.domains.map(item => ({ id: String(item.id).trim(), role: item.role })),
    glossary_ids: form.glossaryIDs.map(item => String(item).trim()),
    responsibilities: form.responsibilities.map(item => ({
      role: item.role,
      subject_type: RESPONSIBILITY_SUBJECT_TYPES[item.role],
      subject_id: String(item.subjectId).trim()
    }))
  }
  return payload
}

export function buildWithdrawCurationPayload(entry) {
  return {
    version: Number(entry?.version || 0),
    business_name: null,
    business_description: null,
    governance_status: 'discovered',
    visibility: 'inventory',
    domains: [],
    glossary_ids: [],
    responsibilities: []
  }
}

export function curationAction(status) {
  if (status === 'discovered') return 'start'
  if (status === 'curated') return 'edit'
  return ''
}

export function buildCertificationPayload(entry) {
  return {
    version: Number(entry?.version || 0),
    governance_status: 'certified'
  }
}

export function buildCertificationWithdrawalPayload(entry, reason) {
  return {
    version: Number(entry?.version || 0),
    governance_status: 'curated',
    reason: nullableTrimmed(reason)
  }
}

export function buildDeprecationPayload(entry, reason, recommendedSuccessorEntryId) {
  return {
    version: Number(entry?.version || 0),
    governance_status: 'deprecated',
    reason: nullableTrimmed(reason),
    recommended_successor_entry_id: nullableTrimmed(recommendedSuccessorEntryId)
  }
}

export function hasEffectivePrimaryDomain(form) {
  if (form.ownerManagedSemantics) {
    if (isCanonicalPositiveID(form.ownerPrimaryDomainId)) return true
    return form.ownerModule === 'standard' && ['platform', 'tenant_common'].includes(form.ownerScopeType)
  }
  return form.domains.filter(item => item.role === 'primary' && isCanonicalPositiveID(item.id)).length === 1
}

export function responsibilitySubjectType(role) {
  return RESPONSIBILITY_SUBJECT_TYPES[role] || ''
}

export function isCanonicalPositiveID(value) {
  return /^[1-9][0-9]*$/.test(String(value || '').trim())
}

export function isCanonicalUUID(value) {
  const normalized = String(value || '').trim()
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(normalized) &&
    normalized !== '00000000-0000-0000-0000-000000000000'
}

function nullableTrimmed(value) {
  const trimmed = String(value || '').trim()
  return trimmed || null
}
