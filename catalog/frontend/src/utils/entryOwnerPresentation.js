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
