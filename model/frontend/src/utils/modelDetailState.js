export const resolvePositiveRouteId = value => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : null
}

export const isEditableDraft = (status, canUpdate) => status === 'draft' && Boolean(canUpdate)

export const canPerformDraftAction = (status, hasActionPermission) =>
  status === 'draft' && Boolean(hasActionPermission)

export const snapshotUnsavedState = state => JSON.stringify(state ?? null)

const elementRevisionKey = binding => `${binding.element_id}/${binding.element_revision_id}`

export const loadModelElementReferences = async (api, bindings) => {
  const frozenBindings = [...new Map(bindings
    .filter(binding => binding.element_id && binding.element_revision_id)
    .map(binding => [elementRevisionKey(binding), binding])).values()]
  const [elementsResult, ...revisionResults] = await Promise.allSettled([
    api.listAll(),
    ...frozenBindings.map(binding => api.getRevision(binding.element_id, binding.element_revision_id))
  ])
  const revisions = new Map()
  revisionResults.forEach((result, index) => {
    if (result.status === 'fulfilled') revisions.set(elementRevisionKey(frozenBindings[index]), result.value)
  })
  return {
    elements: elementsResult.status === 'fulfilled' ? elementsResult.value : [],
    revisions,
    unavailable: [elementsResult, ...revisionResults].some(result => result.status === 'rejected')
  }
}

export const buildModelElementOptions = (elements, selectedId) => elements.flatMap(element => {
  const revision = element.current_revision
  const selectable = element.lifecycle_state === 'active' && revision?.status === 'published'
  if (!selectable && element.id !== selectedId) return []
  return [{
    id: element.id,
    label: revision ? `${revision.name} (${element.code})` : element.code,
    disabled: !selectable
  }]
})

export const getModelElementName = (binding, elements, revisions) => {
  if (binding.element_revision_id) return revisions.get(elementRevisionKey(binding))?.name
  const element = elements.find(item => item.id === binding.element_id)
  return element?.current_revision?.name || element?.code
}

export const applyModelElementToField = (form, elements, elementId) => {
  const element = elements.find(item => item.id === elementId)
  const revision = element?.current_revision
  if (element?.lifecycle_state !== 'active' || revision?.status !== 'published') return
  form.name = revision.name
  form.data_type = revision.data_type
  form.length = revision.length ?? null
}

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
