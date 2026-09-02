export function normalizeMetaItemMetadata(item) {
  if (!item || typeof item !== 'object') return null

  const attributes = item.attributes && typeof item.attributes === 'object' && !Array.isArray(item.attributes)
    ? Object.entries(item.attributes)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, value]) => ({ key, value }))
    : []

  return {
    fingerprint: String(item.fingerprint || '').trim(),
    item_type: String(item.item_type || '').trim(),
    item_type_i18n_key: item.item_type ? `engine.term.${item.item_type}` : '',
    full_name: String(item.full_name || '').trim(),
    row_count: item.row_count ?? null,
    attributes,
    scanned_at: item.scanned_at || null,
    scanned_depth: String(item.scanned_depth || '').trim()
  }
}
