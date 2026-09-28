import { buildBusinessDomainOptions, businessDomainReferenceOptions } from '@common-ui'

// Standard supplies the complete hierarchy; Catalog facets supply counts only for visible entries.
export function domainBrowseOptions(domainTree, domainFacet, selectedId, unresolvedLabel) {
  const facetOptions = domainFacet?.options || []
  const counts = new Map(facetOptions.map(option => [String(option.id), option.count]))
  const options = Array.isArray(domainTree)
    ? buildBusinessDomainOptions(domainTree).map(domain => ({
      ...domain,
      count: domainFacet?.status === 'unavailable' ? undefined : counts.get(String(domain.id)) || 0
    }))
    : businessDomainReferenceOptions(facetOptions)
  const selected = String(selectedId || '')
  if (selected && !options.some(option => String(option.id) === selected)) {
    options.unshift({ id: selected, name: unresolvedLabel, path: [], depth: 0, count: undefined })
  }
  return options
}
