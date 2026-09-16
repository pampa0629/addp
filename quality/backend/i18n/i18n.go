package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgRuleNotFound        = "quality.rule.not_found"
	MsgRuleReferenced      = "quality.rule.referenced"
	MsgVersionConflict     = "quality.request.version_conflict"
	MsgPlanNotFound        = "quality.plan.not_found"
	MsgIssueNotFound       = "quality.issue.not_found"
	MsgIssueUpdateFailed   = "quality.issue.update_failed"
	MsgIssueStatusInvalid  = "quality.issue.status_invalid"
	MsgInvalidRequest      = "quality.request.invalid"
	MsgConflict            = "quality.request.conflict"
	MsgInternal            = "quality.internal.error"
	MsgExecutionListFailed = "quality.execution.list_failed"
	MsgExecutionNotFound   = "quality.execution.not_found"
	MsgDeleted             = "quality.operation.deleted"
	MsgUpdated             = "quality.operation.updated"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
