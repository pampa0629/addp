const validStatuses = new Set(['draft', 'approved', 'deprecated'])

export const parsePositiveInteger = (value, fallback) => {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

export const resolveGlossaryFilters = (query = {}) => ({
  keyword: typeof query.keyword === 'string' ? query.keyword : '',
  domain_id: parsePositiveInteger(query.domain_id, null),
  status: typeof query.status === 'string' && validStatuses.has(query.status) ? query.status : '',
  page: parsePositiveInteger(query.page, 1),
  page_size: parsePositiveInteger(query.page_size, 20)
})

export const buildGlossaryFilterQuery = filters => {
  const query = {}
  if (filters.keyword) query.keyword = filters.keyword
  if (filters.domain_id) query.domain_id = String(filters.domain_id)
  if (filters.status) query.status = filters.status
  if (filters.page > 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  return query
}

export const createGlossaryForm = domainID => ({
  name: '',
  alias: [],
  domain_id: domainID,
  definition: '',
  example: '',
  note: '',
  tags: []
})
