export const resolvePositiveRouteId = value => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : null
}

export const isEditableDraft = (status, canUpdate) => status === 'draft' && Boolean(canUpdate)

export const canPerformDraftAction = (status, hasActionPermission) =>
  status === 'draft' && Boolean(hasActionPermission)

export const snapshotUnsavedState = state => JSON.stringify(state ?? null)

const buildMaterializationRequest = materialization => {
  const targetParentLocator = String(materialization?.target_parent_locator || '').trim()
  const targetName = String(materialization?.target_name || '').trim()
  if (!targetParentLocator && !targetName) return {}
  return {
    target_parent_locator: targetParentLocator,
    target_name: targetName
  }
}

export const buildDDLPreviewRequest = materialization => ({
  materialization: buildMaterializationRequest(materialization)
})

export const buildLogicalTableUpdateRequest = (form, table, materialization) => ({
  ...form,
  version: table?.version,
  domain_id: form.domain_id ?? null,
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
  sort_order: form.sort_order ?? 0
})

export const buildDWLayerUpdateRequest = (form, layer) => ({
  ...form,
  version: layer?.version,
  sort_order: form.sort_order ?? 0
})
