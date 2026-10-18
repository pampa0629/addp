export const protectionPolicyScopeKey = scope => scope ? `${scope.consumer_owner}/${scope.action}` : ''
