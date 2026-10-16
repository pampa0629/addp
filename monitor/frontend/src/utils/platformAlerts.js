// Stable platform-management state; opaque IDs are never inferred from names.
export function platformAlertState(query = {}) {
  const positive = value => typeof value === 'string' && /^[1-9][0-9]*$/u.test(value) && Number(value) <= 10000 ? Number(value) : 1
  return { tab: query.tab === 'notifications' ? 'notifications' : 'incidents',
    category: ['log_pipeline', 'resource'].includes(query.category) ? query.category : '',
    status: query.status === 'all' ? '' : ['open', 'acknowledged', 'resolved'].includes(query.status) ? query.status : 'active',
    subject_id: typeof query.subject_id === 'string' && query.subject_id.length <= 100 && !/[\r\n\u0000]/u.test(query.subject_id) ? query.subject_id : '',
    page: positive(query.page), delivery_page: positive(query.delivery_page) }
}
export function platformAlertQuery(state) {
  return { ...(state.tab === 'notifications' ? { tab: state.tab } : {}), ...(state.category ? { category: state.category } : {}),
    ...(state.status === 'active' ? {} : { status: state.status || 'all' }), ...(state.subject_id ? { subject_id: state.subject_id } : {}),
    ...(state.page > 1 ? { page: String(state.page) } : {}), ...(state.delivery_page > 1 ? { delivery_page: String(state.delivery_page) } : {}) }
}
