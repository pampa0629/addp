package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidID          = "asset.common.invalid_id"
	MsgTypeNotFound       = "asset.type.not_found"
	MsgTypeReadOnly       = "asset.type.read_only"
	MsgCatalogNotFound    = "asset.catalog.not_found"
	MsgDeleteSuccess      = "asset.common.delete_success"
	MsgAssetNotFound      = "asset.asset.not_found"
	MsgAssetPublished     = "asset.asset.published"
	MsgAssetOfflined      = "asset.asset.offlined"
	MsgApproveSuccess     = "asset.application.approve_success"
	MsgRejectSuccess      = "asset.application.reject_success"
	MsgRevokeSuccess      = "asset.authorization.revoke_success"
	MsgUpdateSuccess      = "asset.common.update_success"
	MsgMissingAssetID     = "asset.rating.missing_asset_id"
	MsgInvalidTypeID      = "asset.type.invalid_type_id"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
