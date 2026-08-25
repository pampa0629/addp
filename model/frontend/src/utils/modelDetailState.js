export const resolvePositiveRouteId = value => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : null
}

export const isEditableDraft = (status, canUpdate) => status === 'draft' && Boolean(canUpdate)

export const canPerformDraftAction = (status, hasActionPermission) =>
  status === 'draft' && Boolean(hasActionPermission)

export const snapshotUnsavedState = state => JSON.stringify(state ?? null)

export const buildDDLPreviewRequest = materialization => ({
  materialization: {
    target_parent_locator: String(materialization?.target_parent_locator || '').trim(),
    target_name: String(materialization?.target_name || '').trim(),
    partition_by: String(materialization?.partition_by || '').trim(),
    partition_type: String(materialization?.partition_type || '').trim().toLowerCase()
  }
})

export const buildLogicalTableUpdateRequest = (form, table, materialization) => ({
  ...form,
  version: table?.version,
  domain_id: form.domain_id ?? null,
  entity_id: table?.entity_id ?? null,
  materialization: {
    target_parent_locator: String(materialization?.target_parent_locator || '').trim(),
    target_name: String(materialization?.target_name || '').trim(),
    partition_by: String(materialization?.partition_by || '').trim(),
    partition_type: String(materialization?.partition_type || '').trim().toLowerCase()
  }
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
  sort_order: form.sort_order ?? 0,
  hierarchy_id: form.hierarchy_id ?? null,
  hierarchy_level: form.hierarchy_level ?? null
})

export const buildDWLayerUpdateRequest = (form, layer) => ({
  ...form,
  version: layer?.version,
  quality_sla: layer?.quality_sla ?? null,
  sort_order: form.sort_order ?? 0
})
