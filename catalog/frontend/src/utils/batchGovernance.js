export const BATCH_GOVERNANCE_MAX_ENTRIES = 200
export const BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN = 'assign_primary_domain'
export const BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT = 'assign_accountable_department'

const ownerManagedPrimaryDomainTypes = new Set(['business_entity', 'logical_model', 'metric'])

export function unsupportedPrimaryDomainEntries(rows = []) {
  return rows.filter(row => ownerManagedPrimaryDomainTypes.has(row?.entry_type))
}

export function buildBatchGovernancePayload(rows, operation, referenceID) {
  if (!Array.isArray(rows) || rows.length < 1 || rows.length > BATCH_GOVERNANCE_MAX_ENTRIES) {
    throw new Error('invalid batch governance member count')
  }
  if (![BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN, BATCH_GOVERNANCE_ASSIGN_ACCOUNTABLE_DEPARTMENT].includes(operation)) {
    throw new Error('invalid batch governance operation')
  }
  const normalizedReferenceID = String(referenceID || '')
  if (!/^[1-9]\d*$/.test(normalizedReferenceID)) throw new Error('invalid batch governance reference')
  const seen = new Set()
  const entries = rows.map(row => {
    const id = String(row?.id || '')
    const version = Number(row?.version)
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id) || id === '00000000-0000-0000-0000-000000000000' || !Number.isSafeInteger(version) || version < 1 || seen.has(id)) {
      throw new Error('invalid batch governance member')
    }
    seen.add(id)
    return { id, version }
  })
  if (operation === BATCH_GOVERNANCE_ASSIGN_PRIMARY_DOMAIN && unsupportedPrimaryDomainEntries(rows).length > 0) {
    throw new Error('owner-managed primary domain')
  }
  return { entries, operation, reference_id: normalizedReferenceID }
}
