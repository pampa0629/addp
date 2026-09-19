function positiveId(value) {
  if (!['string', 'number'].includes(typeof value) || !/^[1-9]\d*$/.test(String(value))) {
    throw new Error('A positive standard element or revision ID is required')
  }
  return String(value)
}

export function buildStandardElementRevisionLocation(elementId, revisionId) {
  return { path: `/elements/${positiveId(elementId)}`, query: { revision_id: positiveId(revisionId) } }
}

export function buildStandardElementRevisionRoute(elementId, revisionId) {
  const { path, query } = buildStandardElementRevisionLocation(elementId, revisionId)
  return `/standard${path}?${new URLSearchParams(query)}`
}
