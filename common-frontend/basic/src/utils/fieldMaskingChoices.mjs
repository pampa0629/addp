export const maskingFieldKey = field => `${field.target.resource_identity}\u0000${field.component_key}`
export const currentMaskingChoice = (field, choices) => choices.find(choice => maskingFieldKey(choice) === maskingFieldKey(field))
export const isCurrentMaskingChoice = (field, choice) => Boolean(choice && field.policy_id === choice.selection.policy_id
  && field.assessment_revision === choice.selection.assessment_revision && field.policy_revision === choice.selection.policy_revision
  && field.options.some(option => option.id === choice.selection.option_id))
export function setMaskingChoice(fields, choices, field, optionId) {
  const next = choices.filter(choice => maskingFieldKey(choice) !== maskingFieldKey(field))
  if (optionId) {
    if (!fields.includes(field) || !field.options.some(option => option.id === optionId)) throw new Error('unavailable masking option')
    next.push({ target: { ...field.target }, component_key: field.component_key, selection: {
      policy_id: field.policy_id, assessment_revision: field.assessment_revision, policy_revision: field.policy_revision, option_id: optionId
    } })
  }
  return next
}
