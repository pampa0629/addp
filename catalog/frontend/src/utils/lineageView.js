export function resolveLineageSubject(entry) {
  const source = entry?.source
  if (entry?.entry_status !== 'active' || source?.source_module !== 'meta' ||
      source?.source_type !== 'data_item' || source?.source_status !== 'active') {
    return null
  }
  const rawItemId = source.observed_snapshot?.item_id
  const itemId = typeof rawItemId === 'number'
    ? (Number.isSafeInteger(rawItemId) && rawItemId > 0 ? String(rawItemId) : '')
    : String(rawItemId ?? '').trim()
  if (!/^[1-9]\d*$/.test(itemId)) return null
  return {
    subject_kind: 'data_item',
    item_id: itemId,
    direction: 'both',
    depth: 3,
    limit: 100
  }
}

export function lineageFailureState(error) {
  if (error?.response?.status === 403) return 'forbidden'
  if (error?.response?.status === 404) return 'subject_missing'
  return 'unavailable'
}
