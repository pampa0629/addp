function matchesContext(expected, actual) {
  return !expected || expected === 'any' || expected === actual
}

function matchesPermissions(required, granted) {
  return !required?.length || required.some(permission => granted.has(permission))
}

export function matchesNavigationAccess(entry, contextType, grantedPermissions = []) {
  const granted = grantedPermissions instanceof Set ? grantedPermissions : new Set(grantedPermissions)
  if (entry.access?.length) {
    return entry.access.some(rule =>
      matchesContext(rule.context, contextType) && matchesPermissions(rule.permissions, granted)
    )
  }
  if (entry.contexts?.length && !entry.contexts.includes(contextType)) return false
  return matchesPermissions(entry.permissions, granted)
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
    if (!menu.items) return [module, menu]
    const items = menu.items
      .map(item => filterMenuItem(item, contextType, granted))
      .filter(Boolean)
    return [module, { ...menu, items }]
  }))
}
