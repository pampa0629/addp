export const sections = ['classes', 'properties', 'relations', 'rules']
export const limits = {
  classes: 64,
  properties: 256,
  relations: 128,
  rules: 64
}
export const validID = (value) =>
  typeof value === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value)
export const positiveNumber = (value) =>
  /^[1-9][0-9]*$/.test(String(value)) && Number.isSafeInteger(Number(value))
export const emptyDefinition = () => ({
  classes: [],
  properties: [],
  relations: [],
  rules: []
})
export const definitionInput = (value) =>
  JSON.parse(
    JSON.stringify(
      Object.fromEntries(sections.map((key) => [key, value[key] || []]))
    )
  )
export function newMember(section) {
  return {
    classes: { id: '', name: '', parents: [] },
    properties: {
      id: '',
      name: '',
      class_id: '',
      key: '',
      kind: 'string',
      enum: []
    },
    relations: { id: '', name: '', from: '', to: '', transitive: false },
    rules: { id: '', class_id: '', expression: '', basis: '', inputs: [] }
  }[section]
}
export function availableActions(revision, projection, permissions) {
  const has = (permission) => permissions.includes(permission)
  const update = has('ontology.revision.update'),
    publish = has('ontology.revision.publish')
  const execute = publish && has('system.execution_authorization.create')
  return {
    save: update && revision?.status === 'draft',
    submit: update && revision?.status === 'draft',
    return: update && revision?.status === 'in_review',
    publish: execute && revision?.status === 'in_review',
    withdraw: publish && revision?.status === 'published',
    rebuild:
      execute &&
      revision?.status === 'published' &&
      projection?.status === 'failed'
  }
}
export const isActive = (head, projection) =>
  Boolean(
    projection?.status === 'ready' &&
      head?.active_generation === projection.generation &&
      head?.active_revision === projection.revision
  )
// A write may already be committed when its response is lost. Never replay it.
export const requiresReconciliation = (error) =>
  !error?.response ||
  error.response.status >= 500 ||
  error.response.status === 409
