package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgRuleAppNotFound    = "quality.rule_app.not_found"
	MsgRuleAppCreateFailed = "quality.rule_app.create_failed"
	MsgRuleAppDeleteFailed = "quality.rule_app.delete_failed"
	MsgCheckTaskNotFound  = "quality.check_task.not_found"
	MsgCheckTaskRunFailed = "quality.check_task.run_failed"
	MsgCheckTaskDeleteFailed = "quality.check_task.delete_failed"
	MsgIssueNotFound      = "quality.issue.not_found"
	MsgIssueUpdateFailed  = "quality.issue.update_failed"
	MsgIssueStatusInvalid = "quality.issue.status_invalid"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
