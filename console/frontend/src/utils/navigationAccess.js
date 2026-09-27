import { consoleRouteAccess } from '@common-ui'

function matchesContext(expected, actual) {
  return !expected || expected === 'any' || expected === actual
}

function matchesPermissions(required, granted, mode) {
  if (!required?.length) return true
  return mode === 'all' ? required.every(permission => granted.has(permission)) : required.some(permission => granted.has(permission))
}

export function matchesNavigationAccess(entry, contextType, grantedPermissions = []) {
  const granted = grantedPermissions instanceof Set ? grantedPermissions : new Set(grantedPermissions)
  const routeRules = consoleRouteAccess(entry.index || entry.route)
  const access = routeRules || entry.access
  if (access?.length) {
    return access.some(rule =>
      matchesContext(rule.context, contextType) && matchesPermissions(rule.permissions, granted, rule.permissionMode)
    )
  }
  if (entry.contexts?.length && !entry.contexts.includes(contextType)) return false
  return matchesPermissions(entry.permissions, granted, entry.permissionMode)
}

function filterMenuItem(item, contextType, granted) {
  if (!matchesNavigationAccess(item, contextType, granted)) return null
  if (!item.children) return item
  const children = item.children
    .map(child => filterMenuItem(child, contextType, granted))
    .filter(Boolean)
  return children.length ? { ...item, children } : null
}

export function filterSidebarMenus(menus, contextType, grantedPermissions = []) {
  const granted = new Set(grantedPermissions)
  return Object.fromEntries(Object.entries(menus).map(([module, menu]) => {
    if (!menu.items) return [module, matchesNavigationAccess(menu, contextType, granted) ? menu : null]
    const items = menu.items
      .map(item => filterMenuItem(item, contextType, granted))
      .filter(Boolean)
    const visibleIndexes = new Set(items.map(item => item.index))
    return [module, { ...menu, items: items.filter(item => !item.fallbackFor || !visibleIndexes.has(item.fallbackFor)) }]
  }))
}
