package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidParams                   = "catalog.error.invalid_params"
	MsgEntryNotFound                   = "catalog.error.entry_not_found"
	MsgOperationFailed                 = "catalog.error.operation_failed"
	MsgVersionConflict                 = "catalog.error.version_conflict"
	MsgEntryNotEditable                = "catalog.error.entry_not_editable"
	MsgBatchGovernanceUnsupportedEntry = "catalog.error.batch_governance_unsupported_entry"
	MsgInvalidTransition               = "catalog.error.invalid_transition"
	MsgReferenceNotReferenceable       = "catalog.error.reference_not_referenceable"
	MsgCertificationPermissionRequired = "catalog.error.certification_permission_required"
	MsgDeprecationPermissionRequired   = "catalog.error.deprecation_permission_required"
	MsgReferenceValidationUnavailable  = "catalog.error.reference_validation_unavailable"
	MsgSourceRebindConflict            = "catalog.error.source_rebind_conflict"
	MsgSearchUnavailable               = "catalog.error.search_unavailable"
	MsgInventoryPermissionRequired     = "catalog.error.inventory_permission_required"
	MsgCurationRequirementsNotMet      = "catalog.error.curation_requirements_not_met"
	MsgDeprecationReasonRequired       = "catalog.error.deprecation_reason_required"
	MsgInvalidRecommendedSuccessor     = "catalog.error.invalid_recommended_successor"
	MsgUserPrincipalRequired           = "catalog.error.user_principal_required"
	MsgCollectionNotFound              = "catalog.error.collection_not_found"
	MsgCollectionVersionConflict       = "catalog.error.collection_version_conflict"
	MsgCollectionNameConflict          = "catalog.error.collection_name_conflict"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
