import { parseLocator } from '../../../../common-frontend/basic/src/types/resourceLocator.js'

// Freeze the exact scope reviewed by the user; never infer an engine scan from
// an empty picker or turn a leaf locator into a parent catalog path.
export function prepareManualScan({ engine, scope = null, selection = null, scanDepth = 'basic', force = false }) {
  if (!Number.isSafeInteger(engine?.id) || engine.id <= 0 ||
      !['basic', 'deep'].includes(scanDepth) || typeof force !== 'boolean' || (scope && selection)) {
    throw new Error('invalid_manual_scan')
  }
  const payload = { engine_id: engine.id, scan_depth: scanDepth, force, trigger_type: 'manual', source: 'meta' }
  if (scope?.kind === 'catalog') {
    if (!Array.isArray(scope.paths) || !scope.paths.length ||
        scope.paths.some(path => typeof path !== 'string' || !path.trim())) throw new Error('invalid_manual_scan')
    payload.catalog_paths = Object.freeze([...scope.paths])
  } else if (scope?.kind === 'engine') {
    // Whole-engine scope must be an explicit choice.
  } else if (!scope && selection?.identity?.locator) {
    const locator = selection.identity.locator
    if (parseLocator(locator).engineId !== engine.id) throw new Error('invalid_manual_scan')
    payload.targets = Object.freeze([locator])
  } else {
    throw new Error('invalid_manual_scan')
  }
  return Object.freeze(payload)
}
