export function standardMappingElementLabel(row, t) {
  const reference = row?.element_reference
  if (reference?.status === 'resolved') return reference.code ? `${reference.name} · ${reference.code}` : reference.name
  return row?.evidence?.legacy_observed_snapshot?.name || t('catalog.mapping.elementUnavailable')
}

export function standardMappingRevisionLabel(row, t) {
  if (!row?.element_revision_id) return t('catalog.mapping.revisionMissing')
  return row.element_reference?.status === 'resolved'
    ? `R${row.element_reference.revision_no}`
    : t('catalog.mapping.revisionUnavailable')
}
