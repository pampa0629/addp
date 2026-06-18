import { parseLocatorSafe } from '@addp/common-frontend'

export function parseTransferLocator(locator) {
  const loc = parseLocatorSafe(locator)
  return {
    engineID: loc.engineId,
    path: loc.path || [],
    type: String(loc.type || '').toLowerCase(),
    itemID: Number(loc.itemId || 0)
  }
}
