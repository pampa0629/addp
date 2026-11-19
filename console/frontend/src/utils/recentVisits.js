import { splitConsoleRoute } from './consoleNavigation'

export function recentVisitStorageKey(authContext) {
  const { principal, context } = authContext || {}
  if (principal?.type !== 'user' || !/^[1-9][0-9]*$/.test(principal.id)) return ''
  const prefix = `addp_recent_visits_v3:user:${principal.id}`
  if (context?.type === 'platform') return `${prefix}:platform`
  if (context?.type === 'tenant' && /^[1-9][0-9]*$/.test(context.tenant_id)) {
    return `${prefix}:tenant:${context.tenant_id}`
  }
  return ''
}

export function clearUnownedRecentVisits() {
  try {
    window.localStorage.removeItem('addp_recent_visits_v2')
  } catch { /* Storage is optional. */ }
}

export function loadRecentVisits(key) {
  if (!key) return []
  try {
    const entries = JSON.parse(window.localStorage.getItem(key))
    return Array.isArray(entries) ? entries : []
  } catch {
    return []
  }
}

function findMenuItem(items, path) {
  for (const item of items || []) {
    if (item.index === path) return item
    const child = findMenuItem(item.children, path)
    if (child) return child
  }
  return null
}

export function buildRecentVisitEntry({ module, fullPath, menuConfig, descriptor = null }) {
  if (!module || !menuConfig || typeof fullPath !== 'string' || descriptor?.recent === false) return null

  const [pathPart] = splitConsoleRoute(fullPath)
  const item = findMenuItem(menuConfig.items, pathPart)
  const isFlatEntry = menuConfig.flat && menuConfig.index === pathPart
  if (!descriptor && !item && !isFlatEntry) return null

  const title = typeof descriptor?.title === 'string' ? descriptor.title.trim() : ''
  const subject = typeof descriptor?.subject === 'string' ? descriptor.subject.trim() : ''
  if (descriptor && !title) return null

  const entry = {
    key: pathPart,
    route: fullPath,
    label: item?.recentLabel || item?.label || menuConfig.recentLabel || menuConfig.label,
    module,
    icon: module
  }
  if (title) entry.title = title
  if (subject) entry.subject = subject
  return entry
}

export function prependRecentVisit(list, entry, limit = 5) {
  return [entry, ...list.filter(item => item?.key !== entry.key)].slice(0, limit)
}
