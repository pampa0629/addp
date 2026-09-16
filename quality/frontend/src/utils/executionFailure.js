const failureLabelKeys = {
  'quality.authorization.issue_failed': 'quality.execution.failureAuthorizationIssueFailed',
  'quality.execution.lease_expired': 'quality.execution.failureLeaseExpired',
  'quality.execution.timeout': 'quality.execution.failureTimeout',

  'quality.plan.config_invalid': 'quality.execution.failureGateConfigInvalid',
  'quality.plan.read_context_failed': 'quality.execution.failureGateReadContextFailed',
  'quality.plan.unsupported_engine': 'quality.execution.failureGateUnsupportedEngine',
  'quality.plan.authorization_failed': 'quality.execution.failureGateAuthorizationFailed',
  'quality.plan.rule_compile_failed': 'quality.execution.failureGateCompileFailed',
  'quality.plan.sql_execution_failed': 'quality.execution.failureGateSQLFailed',
  'quality.plan.rule_failed': 'quality.execution.failureGateRuleFailed',
  'quality.plan.result_invalid': 'quality.execution.failureGateResultInvalid'
}

export const executionFailureLabel = (execution, t) => {
  if (execution?.status !== 'failed' && execution?.status !== 'timeout') return ''
  const code = execution.error_details?.code
  return t(failureLabelKeys[code] || 'quality.execution.failureUnknown')
}
