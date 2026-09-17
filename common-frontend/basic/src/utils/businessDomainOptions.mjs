// Standard owns the hierarchy; preserve its sibling order and domain identities.
export function buildBusinessDomainOptions(nodes = [], ancestors = []) {
  return nodes.flatMap(({ children, ...domain }) => {
    const path = [...ancestors, domain.name]
    return [
      { ...domain, depth: ancestors.length, path },
      ...buildBusinessDomainOptions(children || [], path)
    ]
  })
}

export function matchesBusinessDomain(option, query) {
  const keyword = query.trim().toLocaleLowerCase()
  return !keyword || [option.name, ...option.path, option.code || ''].join(' / ').toLocaleLowerCase().includes(keyword)
}

// Catalog's paginated candidates and facets carry owner-resolved paths.
// Historical/unresolved labels have no path until the owner resolves them.
export function businessDomainReferenceOptions(items = []) {
  return items.map(item => ({ ...item, path: item.domain_path || [], depth: Math.max(0, (item.domain_path?.length || 0) - 1) }))
}
