export const isPermissionDenied = error =>
  error?.response?.status === 403 || error?.response?.data?.error_code === 'permission_denied'

export const getModelErrorCode = error => {
  if (isPermissionDenied(error)) return 'permission_denied'
  return error?.response?.data?.error_code || (error?.response?.status === 404 ? 'not_found' : 'model_operation_failed')
}

export const getModelErrorMessage = (error, t, fallbackKey) => {
  if (isPermissionDenied(error)) return t('model.common.permission_denied')
  return error?.response?.data?.error || t(fallbackKey)
}
