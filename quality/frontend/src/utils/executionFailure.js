const failureLabelKeys = {
  'quality.authorization.issue_failed': 'quality.execution.failureAuthorizationIssueFailed',
  'quality.authorization.persist_failed': 'quality.execution.failureAuthorizationPersistFailed',
  'quality.execution.authorization_missing': 'quality.execution.failureAuthorizationMissing',
  'quality.execution.authorization_failed': 'quality.execution.failureAuthorizationFailed',
  'quality.execution.unsupported_engine': 'quality.execution.failureUnsupportedEngine',
  'quality.execution.target_connection_failed': 'quality.execution.failureTargetConnectionFailed',
  'quality.execution.config_invalid': 'quality.execution.failureConfigInvalid',
  'quality.execution.rule_snapshot_invalid': 'quality.execution.failureRuleSnapshotInvalid',
  'quality.execution.no_rule_applications': 'quality.execution.failureNoRules',
  'quality.execution.rule_compile_failed': 'quality.execution.failureRuleCompileFailed',
  'quality.execution.sql_execution_failed': 'quality.execution.failureSQLFailed',
  'quality.execution.result_invalid': 'quality.execution.failureResultInvalid',
  'quality.execution.lease_expired': 'quality.execution.failureLeaseExpired',
  'quality.execution.timeout': 'quality.execution.failureTimeout',
  'quality.issue.reconcile_failed': 'quality.execution.failureIssueReconcileFailed'
}

export const executionFailureLabel = (execution, t) => {
  if (execution?.status !== 'failed' && execution?.status !== 'timeout') return ''
  const code = execution.error_details?.code
  return t(failureLabelKeys[code] || 'quality.execution.failureUnknown')
}
