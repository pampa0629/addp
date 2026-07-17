export const EXISTING_RESULT_CONFIRMATION_REQUIRED = 'existing_result_confirmation_required'

export const requiresCurrentResultConfirmation = error => (
  error?.response?.status === 409 &&
  error?.response?.data?.code === EXISTING_RESULT_CONFIRMATION_REQUIRED
)

export const executeWithCurrentResultConfirmation = async (execute, confirm) => {
  try {
    return await execute({})
  } catch (error) {
    if (!requiresCurrentResultConfirmation(error)) throw error
    await confirm()
    return execute({ parameters: { confirm_existing_result: true } })
  }
}

export const toQuickViewConfirmationPayload = payload => ({
  confirm_existing_result: payload?.parameters?.confirm_existing_result === true
})
