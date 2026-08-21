package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Monitor 模块消息 key 常量
const (
	MsgTenantNotFound               = "monitor.auth.tenant_not_found"
	MsgInvalidExecutionID           = "monitor.execution.invalid_id"
	MsgExecutionNotFound            = "monitor.execution.not_found"
	MsgInvalidRuntimeMetricDuration = "monitor.statistics.invalid_runtime_metric_duration"
	MsgModuleNotFound               = "monitor.health.module_not_found"
	MsgInvalidAlertID               = "monitor.alert.invalid_id"
	MsgAlertNotActive               = "monitor.alert.not_active"
	MsgInvalidSuppression           = "monitor.alert.invalid_suppression"
	MsgInvalidAlertRule             = "monitor.alert_rule.invalid"
	MsgInvalidAlertRuleID           = "monitor.alert_rule.invalid_id"
	MsgAlertRuleNotFound            = "monitor.alert_rule.not_found"
	MsgAlertRuleConflict            = "monitor.alert_rule.conflict"
	MsgAlertRuleDeleted             = "monitor.alert_rule.deleted"
	MsgAlertRuleOperationFailed     = "monitor.alert_rule.operation_failed"
	MsgInvalidWebhookDestination    = "monitor.webhook.invalid_destination"
	MsgWebhookDestinationNotFound   = "monitor.webhook.destination_not_found"
	MsgWebhookDestinationConflict   = "monitor.webhook.destination_conflict"
	MsgWebhookDestinationDeleted    = "monitor.webhook.destination_deleted"
	MsgInvalidWebhookDelivery       = "monitor.webhook.invalid_delivery"
	MsgWebhookDeliveryNotFound      = "monitor.webhook.delivery_not_found"
	MsgWebhookDeliveryNotRetryable  = "monitor.webhook.delivery_not_retryable"
	MsgWebhookTestFailed            = "monitor.webhook.test_failed"
	MsgWebhookOperationFailed       = "monitor.webhook.operation_failed"
	MsgInvalidEmailDestination      = "monitor.email.invalid_destination"
	MsgEmailDestinationNotFound     = "monitor.email.destination_not_found"
	MsgEmailDestinationConflict     = "monitor.email.destination_conflict"
	MsgEmailDestinationDeleted      = "monitor.email.destination_deleted"
	MsgInvalidEmailDelivery         = "monitor.email.invalid_delivery"
	MsgEmailDeliveryNotFound        = "monitor.email.delivery_not_found"
	MsgEmailDeliveryNotRetryable    = "monitor.email.delivery_not_retryable"
	MsgEmailSenderUnavailable       = "monitor.email.sender_unavailable"
	MsgEmailTestFailed              = "monitor.email.test_failed"
	MsgEmailOperationFailed         = "monitor.email.operation_failed"
	MsgConfigurationLoadFailed      = "monitor.configuration.load_failed"
	MsgConfigurationInvalid         = "monitor.configuration.invalid"
	MsgConfigurationAuthentication  = "monitor.configuration.authentication_required"
	MsgConfigurationConflict        = "monitor.configuration.version_conflict"
	MsgSMTPRelayLoadFailed          = "monitor.smtp_relay.load_failed"
	MsgSMTPRelayInvalid             = "monitor.smtp_relay.invalid"
	MsgSMTPRelayCredentialRequired  = "monitor.smtp_relay.credential_required"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
