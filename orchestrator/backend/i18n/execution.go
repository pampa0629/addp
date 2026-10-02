package i18n

import commoni18n "github.com/addp/common/middleware/i18n"

// ExecutionFailureMessage owns the closed orchestration failure catalog.
func ExecutionFailureMessage(lang, code string) (string, bool) {
	switch code {
	case "orchestrator.execution.dispatch_denied", "orchestrator.execution.provider_unavailable", "orchestrator.execution.contract_unavailable", "orchestrator.execution.service_auth_unavailable", "orchestrator.execution.plan_invalid", "orchestrator.execution.step_state_invalid", "orchestrator.execution.dispatch_uncertain", "orchestrator.execution.step_timeout", "orchestrator.execution.child_failed", "orchestrator.execution.binding_invalid", "orchestrator.execution.contract_invalid", "orchestrator.execution.dispatch_rejected", "orchestrator.execution.lease_expired", "orchestrator.execution.coordinator_stopped", "orchestrator.execution.plan_missing", "orchestrator.execution.lease_missing":
		return commoni18n.ForLanguage(lang, code), true
	default:
		return "", false
	}
}
