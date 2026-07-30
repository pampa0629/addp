export async function listSystemCatalogChildren(
  httpClient,
  engineId,
  path = { segments: [] },
  options = {}
) {
  const res = await httpClient.post(`/system/engines/${engineId}/catalog/children`, {
    path: normalizeCatalogPath(path),
    options
  })
  if (Array.isArray(res?.nodes)) return res.nodes
  if (Array.isArray(res?.data?.nodes)) return res.data.nodes
  if (Array.isArray(res)) return res
  return []
}

export function normalizeCatalogPath(path = { segments: [] }) {
  return {
    segments: [],
    ...path,
    segments: Array.isArray(path?.segments) ? [...path.segments] : []
  }
}
