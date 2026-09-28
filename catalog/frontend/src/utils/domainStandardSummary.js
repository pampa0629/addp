export const DOMAIN_STANDARD_PAGE_SIZE = 5

export const DOMAIN_STANDARD_SECTIONS = [
  { key: 'glossaries', permission: 'standard.glossary.read', route: '/standard/glossaries' },
  { key: 'elements', permission: 'standard.element.read', route: '/standard/elements' },
  { key: 'metrics', permission: 'standard.metric.read', route: '/standard/metrics' }
]

export function domainStandardQuery(domainId, page) {
  return {
    scope_type: 'domain',
    owner_domain_id: String(domainId),
    page,
    page_size: DOMAIN_STANDARD_PAGE_SIZE
  }
}

export function domainStandardDisplay(item) {
  const revision = item?.current_revision || item?.draft_revision
  return {
    id: item?.id,
    name: revision?.name || item?.code || '',
    code: item?.code || '',
    revisionStatus: revision?.status || ''
  }
}

export async function loadDomainStandardSection(section, { domainId, hasPermission, list }) {
  const version = ++section.requestVersion
  section.items = []
  section.total = 0
  if (!domainId || !hasPermission(section.permission)) {
    section.status = 'forbidden'
    return
  }
  section.status = 'loading'
  try {
    const response = await list(domainStandardQuery(domainId, section.page))
    if (version !== section.requestVersion) return
    section.items = (response.data || []).map(domainStandardDisplay)
    section.total = Number(response.total) || 0
    section.status = 'ready'
  } catch (error) {
    if (version !== section.requestVersion) return
    section.status = error?.response?.status === 403 ? 'forbidden' : 'unavailable'
  }
}
