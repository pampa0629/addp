export const EXISTING_RESULT_ACTION_REQUIRED = 'existing_result_action_required'

export const requiresCurrentResultConfirmation = error => (
  error?.response?.status === 409 &&
  error?.response?.data?.code === EXISTING_RESULT_ACTION_REQUIRED
)

export const executeWithCurrentResultConfirmation = async (execute, confirm) => {
  try {
    return await execute({})
  } catch (error) {
    if (!requiresCurrentResultConfirmation(error)) throw error
    await confirm()
    return execute({ parameters: { existing_result_action: 'overwrite' } })
  }
}

export const toQuickViewExistingResultPayload = payload => {
  const action = payload?.parameters?.existing_result_action
  return action ? { existing_result_action: action } : {}
}
