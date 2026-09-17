// The caller supplies the authenticated shared HTTP client. Construction is lazy;
// Model availability and permissions are evaluated only when a method is called.
export function createModelMetricAPI(client) {
  return {
    list: params => client.get('/model/metric-implementations', { params }),
    get: id => client.get(`/model/metric-implementations/${id}`),
  }
}

export function publishedMetricSources(implementations) {
  if (!Array.isArray(implementations)) throw new TypeError('Invalid Model metric list')
  return implementations.map(item => ({
    id: item.id,
    name: item.name,
    revisions: (item.revisions || []).filter(revision => revision.status === 'published')
      .map(revision => ({ id: revision.id, revision_no: revision.revision_no })),
  })).filter(item => item.revisions.length > 0)
}
