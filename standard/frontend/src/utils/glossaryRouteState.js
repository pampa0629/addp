const validStatuses = new Set(['draft', 'in_review', 'published', 'withdrawn'])

export const parsePositiveInteger = (value, fallback) => {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

export const resolveGlossaryFilters = (query = {}) => ({
  keyword: typeof query.keyword === 'string' ? query.keyword : '',
  owner_domain_id: parsePositiveInteger(query.owner_domain_id, null),
  status: typeof query.status === 'string' && validStatuses.has(query.status) ? query.status : '',
  page: parsePositiveInteger(query.page, 1),
  page_size: parsePositiveInteger(query.page_size, 20)
})

export const buildGlossaryFilterQuery = filters => {
  const query = {}
  if (filters.keyword) query.keyword = filters.keyword
  if (filters.owner_domain_id) query.owner_domain_id = String(filters.owner_domain_id)
  if (filters.status) query.status = filters.status
  if (filters.page > 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  return query
}

export const createGlossaryForm = domainID => ({
  scope_type: domainID ? 'domain' : 'tenant_common',
  owner_domain_id: domainID,
  code: '',
  name: '',
  alias: [],
  definition: '',
  example: '',
  note: '',
  related_ids: [],
  tags: [],
  change_summary: '',
  effective_from: null,
  effective_to: null
})

export const isGlossaryDeletable = glossary => Boolean(glossary) && !glossary.has_publication_history
