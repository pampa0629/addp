export const isPermissionDenied = error =>
  error?.response?.status === 403 || error?.response?.data?.error_code === 'permission_denied'

export const isResourceVersionConflict = error =>
  error?.response?.status === 409 && error?.response?.data?.error_code === 'resource_version_conflict'

export const getModelErrorCode = error => {
  if (isPermissionDenied(error)) return 'permission_denied'
  return error?.response?.data?.error_code || (error?.response?.status === 404 ? 'not_found' : 'model_operation_failed')
}

export const getModelErrorMessage = (error, t, fallbackKey) => {
  if (isPermissionDenied(error)) return t('model.common.permission_denied')
  if (isResourceVersionConflict(error)) return t('model.common.resource_version_conflict')
  return error?.response?.data?.error || t(fallbackKey)
}
