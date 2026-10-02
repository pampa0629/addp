package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgSharingHandlingForbidden              = "catalog.error.sharing_handling_forbidden"
	MsgSharingConfirmationForbidden          = "catalog.error.sharing_confirmation_forbidden"
	MsgSharingDecisionConflict               = "catalog.error.sharing_decision_conflict"
	MsgSharingTargetUnsupported              = "catalog.error.sharing_target_unsupported"
	MsgInvalidParams                         = "catalog.error.invalid_params"
	MsgEntryNotFound                         = "catalog.error.entry_not_found"
	MsgOperationFailed                       = "catalog.error.operation_failed"
	MsgVersionConflict                       = "catalog.error.version_conflict"
	MsgEntryNotEditable                      = "catalog.error.entry_not_editable"
	MsgBatchGovernanceUnsupportedEntry       = "catalog.error.batch_governance_unsupported_entry"
	MsgInvalidTransition                     = "catalog.error.invalid_transition"
	MsgReferenceNotReferenceable             = "catalog.error.reference_not_referenceable"
	MsgCertificationPermissionRequired       = "catalog.error.certification_permission_required"
	MsgCertificationRequirementsNotMet       = "catalog.error.certification_requirements_not_met"
	MsgCertificationWithdrawalReasonRequired = "catalog.error.certification_withdrawal_reason_required"
	MsgDeprecationPermissionRequired         = "catalog.error.deprecation_permission_required"
	MsgReferenceValidationUnavailable        = "catalog.error.reference_validation_unavailable"
	MsgSourceRebindConflict                  = "catalog.error.source_rebind_conflict"
	MsgSearchUnavailable                     = "catalog.error.search_unavailable"
	MsgDomainOverviewUnavailable             = "catalog.error.domain_overview_unavailable"
	MsgInventoryPermissionRequired           = "catalog.error.inventory_permission_required"
	MsgCurationRequirementsNotMet            = "catalog.error.curation_requirements_not_met"
	MsgDeprecationReasonRequired             = "catalog.error.deprecation_reason_required"
	MsgInvalidRecommendedSuccessor           = "catalog.error.invalid_recommended_successor"
	MsgUserPrincipalRequired                 = "catalog.error.user_principal_required"
	MsgCollectionNotFound                    = "catalog.error.collection_not_found"
	MsgCollectionVersionConflict             = "catalog.error.collection_version_conflict"
	MsgCollectionNameConflict                = "catalog.error.collection_name_conflict"
	MsgDataDictionaryNotApplicable           = "catalog.error.data_dictionary_not_applicable"
	MsgDataDictionaryDependencyUnavailable   = "catalog.error.data_dictionary_dependency_unavailable"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
