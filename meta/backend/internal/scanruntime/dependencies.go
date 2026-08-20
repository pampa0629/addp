package scanruntime

import (
	"context"

	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanprocessor"
)

type TableAssetIndexer interface {
	IndexTableAsset(ctx context.Context, resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo datatype.TableInfo, fields []datatype.FieldInfo, item *models.MetaItem)
	DeleteTablesFromIndex(tenantID, engineID uint, schemaName string)
}

type RuntimeIndexer interface {
	scanprocessor.AssetIndexer
	TableAssetIndexer
}
