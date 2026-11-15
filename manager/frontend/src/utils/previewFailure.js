// Reduce transport failures to the facts needed by the shared preview failure panel.
export function previewFailureFromError(error) {
  const status = error.response?.status || 0
  const errorCode = error.response?.data?.error_code
  let timeoutReason = ''
  if (status === 504) {
    timeoutReason = errorCode === 'preview_connection_acquisition_timeout' ? 'connection_acquisition_timeout'
      : errorCode === 'preview_sql_execution_timeout' ? 'sql_execution_timeout' : 'timeout'
  } else if (!status && ['ECONNABORTED', 'ETIMEDOUT'].includes(error.code)) {
    timeoutReason = 'timeout'
  }
  return { status, code: typeof errorCode === 'string' ? errorCode : '', ...(timeoutReason ? { timeoutReason } : {}) }
}
