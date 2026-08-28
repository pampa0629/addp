package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidID               = "asset.common.invalid_id"
	MsgTypeNotFound            = "asset.type.not_found"
	MsgTypeReadOnly            = "asset.type.read_only"
	MsgCategoryNotFound        = "asset.category.not_found"
	MsgCategoryVersionConflict = "asset.category.version_conflict"
	MsgCategoryParentNotFound  = "asset.category.parent_not_found"
	MsgCategoryInvalidParent   = "asset.category.invalid_parent"
	MsgCategoryDuplicateName   = "asset.category.duplicate_name"
	MsgDeleteSuccess           = "asset.common.delete_success"
	MsgAssetNotFound           = "asset.asset.not_found"
	MsgAssetPublished          = "asset.asset.published"
	MsgAssetOfflined           = "asset.asset.offlined"
	MsgApproveSuccess          = "asset.application.approve_success"
	MsgRejectSuccess           = "asset.application.reject_success"
	MsgRevokeSuccess           = "asset.authorization.revoke_success"
	MsgUpdateSuccess           = "asset.common.update_success"
	MsgMissingAssetID          = "asset.rating.missing_asset_id"
	MsgInvalidTypeID           = "asset.type.invalid_type_id"
	MsgCatalogUnavailable      = "asset.asset.catalog_unavailable"
	MsgNotEditable             = "asset.asset.not_editable"
	MsgVersionConflict         = "asset.asset.version_conflict"
	MsgReferenceNotSelectable  = "asset.asset.reference_not_selectable"
	MsgReferenceNotPublishable = "asset.asset.reference_not_publishable"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
