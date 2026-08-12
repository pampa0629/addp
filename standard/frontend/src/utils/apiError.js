export const getStandardErrorMessage = (error, t, fallbackKey = 'standard.common.operationFailed') => {
  const message = error?.response?.data?.error
  return typeof message === 'string' && message.trim() ? message : t(fallbackKey)
}

export const isCanceledInteraction = error => error === 'cancel' || error === 'close'
