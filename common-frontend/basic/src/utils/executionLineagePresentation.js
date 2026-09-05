import { formatLocatorDisplayPath, parseLocatorSafe } from '../types/resourceLocator.js'

export const EXECUTION_LINEAGE_SCHEMA_VERSION = 'addp.lineage-facts/v1'

const EMPTY_SUMMARY = Object.freeze({
  schemaVersion: '',
  inputs: [],
  outputs: [],
  operations: []
})

export function buildExecutionLineageSummary(metadata) {
  const facts = normalizeObject(normalizeObject(metadata).lineage_facts)
  if (facts.schema_version !== EXECUTION_LINEAGE_SCHEMA_VERSION) return EMPTY_SUMMARY

  return {
    schemaVersion: facts.schema_version,
    inputs: normalizeResources(facts.inputs, 'input'),
    outputs: normalizeResources(facts.outputs, 'output'),
    operations: normalizeOperations(facts.operations)
  }
}

function normalizeResources(resources, direction) {
  if (!Array.isArray(resources)) return []
  return resources
    .map((resource, index) => normalizeResource(resource, direction, index))
    .filter(Boolean)
}

function normalizeResource(resource, direction, index) {
  if (!resource || typeof resource !== 'object' || Array.isArray(resource)) return null

  const locator = String(resource.locator || '').trim()
  if (!locator) return null

  const port = String(resource.port || '').trim()
  const presentation = locator.startsWith('addp-infra://')
    ? presentInfraLocator(locator)
    : presentBusinessLocator(locator)

  return {
    key: `${direction}:${port}:${locator}:${index}`,
    direction,
    port,
    locator,
    displayName: presentation.displayName || locator,
    resourceType: presentation.resourceType,
    itemId: positiveInteger(resource.item_id) || presentation.itemId,
    writeMode: String(resource.write_mode || '').trim(),
    platformInternal: presentation.platformInternal,
    explorable: presentation.explorable
  }
}

function presentBusinessLocator(locator) {
  const parsed = parseLocatorSafe(locator)
  if (!parsed.engineId || !parsed.type) {
    return { displayName: locator, resourceType: '', itemId: 0, platformInternal: false, explorable: false }
  }

  return {
    displayName: formatLocatorDisplayPath(locator),
    resourceType: parsed.type,
    itemId: positiveInteger(parsed.itemId),
    platformInternal: false,
    explorable: true
  }
}

function presentInfraLocator(locator) {
  try {
    const parsed = new URL(locator)
    if (parsed.protocol !== 'addp-infra:') throw new Error('unsupported infra locator')
    const segments = parsed.pathname
      .split('/')
      .filter(Boolean)
      .map(segment => decodeURIComponent(segment))
    return {
      displayName: segments.at(-1) || parsed.hostname || locator,
      resourceType: String(parsed.searchParams.get('type') || '').trim(),
      itemId: 0,
      platformInternal: true,
      explorable: false
    }
  } catch {
    return { displayName: locator, resourceType: '', itemId: 0, platformInternal: true, explorable: false }
  }
}

function normalizeOperations(operations) {
  if (!Array.isArray(operations)) return []
  return operations
    .filter(operation => operation && typeof operation === 'object' && !Array.isArray(operation))
    .map(operation => ({
      kind: String(operation.kind || '').trim(),
      operator: String(operation.operator || '').trim(),
      inputPorts: stringArray(operation.input_ports),
      outputPorts: stringArray(operation.output_ports)
    }))
}

function stringArray(value) {
  return Array.isArray(value) ? value.map(item => String(item || '').trim()).filter(Boolean) : []
}

function positiveInteger(value) {
  const normalized = Number(value)
  return Number.isInteger(normalized) && normalized > 0 ? normalized : 0
}

function normalizeObject(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return value
}
