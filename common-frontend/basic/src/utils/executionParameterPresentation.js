import { formatLocatorDisplayPath, parseLocatorSafe } from '../types/resourceLocator.js'

export function sortEntriesByOrder(values, uiValues = {}) {
  return Object.entries(values || {}).sort(([leftName], [rightName]) => {
    const leftOrder = Number(uiValues?.[leftName]?.order)
    const rightOrder = Number(uiValues?.[rightName]?.order)
    const normalizedLeft = Number.isFinite(leftOrder) ? leftOrder : Number.MAX_SAFE_INTEGER
    const normalizedRight = Number.isFinite(rightOrder) ? rightOrder : Number.MAX_SAFE_INTEGER
    return normalizedLeft - normalizedRight || leftName.localeCompare(rightName)
  })
}

export function summarizeExecutionResource(field, value, enginesById = {}) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return { status: 'empty', engineId: 0, engineName: '', name: '', type: '' }
  }

  const binding = field?.ui?.resource_binding || {}
  const locatorName = binding.mode === 'target' ? 'parent_locator' : 'locator'
  const locatorValue = String(value[locatorName] || '').trim()
  if (!locatorValue) {
    return { status: 'empty', engineId: 0, engineName: '', name: '', type: '' }
  }
  const locator = parseLocatorSafe(locatorValue)
  if (!locator.engineId || !locator.type) {
    return { status: 'configured', engineId: 0, engineName: '', name: '', type: '' }
  }

  const engine = enginesById[locator.engineId] || enginesById[String(locator.engineId)] || {}
  const appendedPath = []
  let type = locator.type
  if (binding.mode === 'target') {
    const targetName = String(value.name || '').trim()
    if (targetName) appendedPath.push(targetName)
    type = binding.type_values?.[locator.type] || ''
  }

  return {
    status: 'resolved',
    engineId: locator.engineId,
    engineName: engine.name || '',
    name: formatLocatorDisplayPath(locatorValue, {
      engineType: engine.engine_type,
      appendedPath,
      resourceType: type
    }),
    type
  }
}
