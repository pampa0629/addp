import { buildBusinessDomainOptions } from '@common-ui'

const PROFESSIONAL_OWNER_NAMES = Object.freeze({
  model: 'Model',
  standard: 'Standard',
  service: 'Service',
  develop: 'Develop',
  workbench: 'Workbench'
})

export function isProfessionalOwner(sourceModule) {
  return Object.hasOwn(PROFESSIONAL_OWNER_NAMES, String(sourceModule || ''))
}

export function professionalOwnerName(sourceModule) {
  return PROFESSIONAL_OWNER_NAMES[String(sourceModule || '')] || ''
}

export function resolveOwnerPrimaryDomain(domainTree, domainID) {
  if (!Array.isArray(domainTree) || !domainID) return null
  return buildBusinessDomainOptions(domainTree).find(domain => String(domain.id) === String(domainID)) || null
}

// Names are read through the current User's Standard APIs. Changing entries or
// permissions invalidates both results before starting another observation.
export async function loadOwnerMetricReferences(state, { entry, hasPermission, listCategories, getUnit }) {
  const version = ++state.requestVersion
  state.category = null
  state.unit = null
  if (entry?.source?.source_module !== 'standard' || entry?.entry_type !== 'metric') return
  const summary = entry.source_resolution?.summary || entry.source?.observed_snapshot || {}
  const [categories, unit] = await Promise.allSettled([
    summary.category_id && hasPermission('standard.metric.read') ? listCategories() : Promise.resolve([]),
    summary.unit_id && hasPermission('standard.unit.read') ? getUnit(summary.unit_id) : Promise.resolve(null)
  ])
  if (version !== state.requestVersion) return
  if (categories.status === 'fulfilled' && Array.isArray(categories.value)) {
    state.category = categories.value.find(item => String(item.id) === String(summary.category_id)) || null
  }
  if (unit.status === 'fulfilled' && String(unit.value?.id) === String(summary.unit_id)) state.unit = unit.value
}
