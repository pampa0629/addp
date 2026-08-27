const RESPONSIBILITY_SUBJECT_TYPES = Object.freeze({
  accountable_department: 'department',
  business_owner: 'user',
  data_steward: 'user',
  technical_owner: 'user'
})

export function buildEntryEditForm(entry) {
  const semanticLinks = Array.isArray(entry?.semantic_links) ? entry.semantic_links : []
  const ownerManagedSemantics = ['model', 'standard'].includes(entry?.source?.source_module)
  const ownerManagedComponents = ['model', 'standard', 'service', 'develop'].includes(entry?.source?.source_module)
  const componentElements = new Map(
    (Array.isArray(entry?.component_elements) ? entry.component_elements : [])
      .map(link => [String(link.component_id), link.element_id])
  )
  return {
    version: Number(entry?.version || 0),
    businessName: entry?.business_name || '',
    businessDescription: entry?.business_description || '',
    governanceStatus: entry?.governance_status || 'discovered',
    visibility: entry?.visibility || 'inventory',
    ownerManagedSemantics,
    ownerManagedComponents,
    ownerModule: entry?.source?.source_module || '',
    ownerPrimaryDomainId: ownerManagedSemantics
      ? String(entry?.source_resolution?.summary?.domain_id || entry?.source?.observed_snapshot?.domain_id || '')
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
      .map(item => ({ role: item.role, subjectId: String(item.subject_id) })),
    componentElements: (Array.isArray(entry?.components) ? entry.components : [])
      .map(component => ({
        componentId: String(component.id),
        componentName: component.display_name,
        componentStatus: component.component_status,
        elementId: componentElements.has(String(component.id))
          ? String(componentElements.get(String(component.id)))
          : null
      })),
    deprecationReason: '',
    recommendedSuccessorEntryId: entry?.recommended_successor_entry_id || ''
  }
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
    })),
    component_elements: form.ownerManagedComponents ? [] : form.componentElements
      .filter(item => item.elementId !== null && item.elementId !== undefined && item.elementId !== '')
      .map(item => ({ component_id: item.componentId, element_id: String(item.elementId).trim() })),
    recommended_successor_entry_id: nullableTrimmed(form.recommendedSuccessorEntryId)
  }
  const reason = nullableTrimmed(form.deprecationReason)
  if (reason !== null) payload.deprecation_reason = reason
  return payload
}

export function hasEffectivePrimaryDomain(form) {
  if (form.ownerManagedSemantics) return isCanonicalPositiveID(form.ownerPrimaryDomainId)
  return form.domains.filter(item => item.role === 'primary').length === 1
}

export function governanceOptions(current, canCertify, canDeprecate) {
  const result = [current]
  if (current === 'discovered') result.push('curated')
  if (current === 'curated' && canCertify) result.push('certified')
  if ((current === 'curated' || current === 'certified') && canDeprecate) result.push('deprecated')
  return [...new Set(result)]
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
