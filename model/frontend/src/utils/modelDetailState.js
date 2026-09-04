export const resolvePositiveRouteId = value => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : null
}

export const isEditableDraft = (status, canUpdate) => status === 'draft' && Boolean(canUpdate)

export const canPerformDraftAction = (status, hasActionPermission) =>
  status === 'draft' && Boolean(hasActionPermission)

export const snapshotUnsavedState = state => JSON.stringify(state ?? null)

const buildMaterializationRequest = materialization => {
  const partitionBy = String(materialization?.partition_by || '').trim()
  const normalized = {
    target_parent_locator: String(materialization?.target_parent_locator || '').trim(),
    target_name: String(materialization?.target_name || '').trim()
  }
  if (partitionBy) {
    normalized.partition_by = partitionBy
    normalized.partition_type = String(materialization?.partition_type || 'range').trim().toLowerCase()
  }
  return normalized
}

export const buildDDLPreviewRequest = materialization => ({
  materialization: buildMaterializationRequest(materialization)
})

export const buildLogicalTableUpdateRequest = (form, table, materialization) => ({
  ...form,
  version: table?.version,
  domain_id: form.domain_id ?? null,
  entity_id: table?.entity_id ?? null,
  materialization: buildMaterializationRequest(materialization)
})

export const buildEntityAttributeUpdateRequest = (form, version) => ({
  ...form,
  version,
  element_id: form.element_id ?? null,
  is_pk: Boolean(form.is_pk),
  nullable: Boolean(form.nullable),
  sort_order: form.sort_order ?? 0
})

export const buildLogicalFieldUpdateRequest = (form, version) => ({
  ...form,
  version,
  element_id: form.element_id ?? null,
  length: form.length ?? null,
  nullable: Boolean(form.nullable),
  is_pk: Boolean(form.is_pk),
  is_partition: Boolean(form.is_partition),
  sort_order: form.sort_order ?? 0
})

export const buildDWLayerUpdateRequest = (form, layer) => ({
  ...form,
  version: layer?.version,
  quality_sla: layer?.quality_sla ?? null,
  sort_order: form.sort_order ?? 0
})
