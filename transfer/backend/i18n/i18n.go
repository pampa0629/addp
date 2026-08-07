package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgReplayInvalid                  = "transfer.replay.invalid"
	MsgReplayRangeUnavailable         = "transfer.replay.range_unavailable"
	MsgReplayTargetExists             = "transfer.replay.target_exists"
	MsgReplayUnavailable              = "transfer.replay.unavailable"
	MsgReplayInternalError            = "transfer.replay.internal_error"
	MsgTaskNotFound                   = "transfer.task.not_found"
	MsgDeadLetterInvalidQuery         = "transfer.dead_letter.invalid_query"
	MsgDeadLetterInvalidID            = "transfer.dead_letter.invalid_identity"
	MsgDeadLetterNotFound             = "transfer.dead_letter.not_found"
	MsgDeadLetterInternal             = "transfer.dead_letter.internal_error"
	MsgTaskDeleteRequiresStopped      = "transfer.task.delete_requires_stopped"
	MsgTaskDeleteCleanupFailed        = "transfer.task.delete_cleanup_failed"
	MsgSchemaChangeInvalid            = "transfer.schema_change.invalid"
	MsgSchemaChangeNotFound           = "transfer.schema_change.not_found"
	MsgSchemaChangeNotAdditive        = "transfer.schema_change.not_additive"
	MsgSchemaChangeConflict           = "transfer.schema_change.conflict"
	MsgSchemaChangeUnavailable        = "transfer.schema_change.unavailable"
	MsgSchemaChangeInternal           = "transfer.schema_change.internal_error"
	MsgFieldRecommendationInvalid     = "transfer.field_recommendation.invalid"
	MsgFieldRecommendationUnsupported = "transfer.field_recommendation.unsupported"
	MsgFieldRecommendationUnavailable = "transfer.field_recommendation.unavailable"
	MsgConfigurationLoadFailed        = "transfer.configuration.load_failed"
	MsgConfigurationInvalid           = "transfer.configuration.invalid"
	MsgConfigurationAuthentication    = "transfer.configuration.authentication_required"
	MsgConfigurationConflict          = "transfer.configuration.version_conflict"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
