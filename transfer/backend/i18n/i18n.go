package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgReplayInvalid             = "transfer.replay.invalid"
	MsgReplayRangeUnavailable    = "transfer.replay.range_unavailable"
	MsgReplayTargetExists        = "transfer.replay.target_exists"
	MsgReplayUnavailable         = "transfer.replay.unavailable"
	MsgReplayInternalError       = "transfer.replay.internal_error"
	MsgTaskNotFound              = "transfer.task.not_found"
	MsgDeadLetterInvalidQuery    = "transfer.dead_letter.invalid_query"
	MsgDeadLetterInvalidID       = "transfer.dead_letter.invalid_identity"
	MsgDeadLetterNotFound        = "transfer.dead_letter.not_found"
	MsgDeadLetterInternal        = "transfer.dead_letter.internal_error"
	MsgTaskDeleteRequiresStopped = "transfer.task.delete_requires_stopped"
	MsgTaskDeleteCleanupFailed   = "transfer.task.delete_cleanup_failed"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
