export const STANDARD_PERMISSION_RESOURCES = Object.freeze([
  'classification',
  'code_set',
  'dimension_hierarchy',
  'document',
  'domain',
  'element',
  'glossary',
  'metric',
  'unit'
])

export function buildStandardPermission(resource, action) {
  if (!STANDARD_PERMISSION_RESOURCES.includes(resource)) {
    throw new Error(`Unknown Standard permission resource: ${resource}`)
  }
  return `standard.${resource}.${action}`
}
