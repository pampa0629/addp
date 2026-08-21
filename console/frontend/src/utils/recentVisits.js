import { splitConsoleRoute } from './consoleNavigation'

export function buildRecentVisitEntry({ module, fullPath, menuConfig, descriptor = null }) {
  if (!module || !menuConfig || typeof fullPath !== 'string' || descriptor?.recent === false) return null

  const [pathPart] = splitConsoleRoute(fullPath)
  const item = menuConfig.items?.find(candidate => candidate.index === pathPart)
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
  if (!entry) return Array.isArray(list) ? list : []
  const current = Array.isArray(list) ? list : []
  return [entry, ...current.filter(item => item.key !== entry.key)].slice(0, limit)
}
