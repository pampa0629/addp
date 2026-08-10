export const isPermissionDenied = error =>
  error?.response?.status === 403 || error?.response?.data?.error_code === 'permission_denied'

export const getModelErrorMessage = (error, t, fallbackKey) => {
  if (isPermissionDenied(error)) return t('model.common.permission_denied')
  return error?.response?.data?.error || t(fallbackKey)
}
