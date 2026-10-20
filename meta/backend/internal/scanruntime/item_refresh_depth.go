package scanruntime

import (
	"context"
	"fmt"
	"reflect"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

// Basic refresh consumes only the exact catalog entry's cheap summary. It does
// not call Describe, sample records, enumerate siblings or rebuild deep fields.
func (r *ItemRefreshRuntime) refreshKnownCatalogSummary(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	p plugin.EnginePlugin,
	item models.MetaItem,
	parent models.MetaNode,
	path plugin.EngineCatalogPath,
) (scanprocessor.Result, error) {
	catalog, ok := p.(plugin.EngineCatalogProvider)
	if !ok {
		return scanprocessor.Result{}, fmt.Errorf("basic item refresh requires EngineCatalogProvider")
	}
	entry, err := catalog.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path)
	if err != nil {
		return scanprocessor.Result{}, fmt.Errorf("resolve basic item refresh target: %w", err)
	}
	if entry == nil || !reflect.DeepEqual(entry.Path, path) || entry.Name != item.Name || entry.Role != plugin.EngineCatalogRoleLeaf {
		return scanprocessor.Result{}, fmt.Errorf("basic item refresh target was not resolved exactly")
	}
	attrs := metaattr.JSONMap(cloneJSONMap(item.Attributes))
	rowCount, sizeBytes, updatedAt := item.RowCount, item.SizeBytes, item.DataUpdatedAt
	summary := map[string]interface{}{}
	if entry.Table != nil {
		// Table on EngineCatalogEntry is a summary, never a source of Fields,
		// PrimaryKey, Native, indexes or other deep metadata, even if populated.
		if entry.Table.RowCount != nil {
			rowCount = entry.Table.RowCount
			summary["row_count"] = *rowCount
		}
		if entry.Table.EstimatedRowCount != nil {
			summary["estimated_row_count"] = *entry.Table.EstimatedRowCount
		}
		if entry.Table.SizeBytes != nil {
			sizeBytes = entry.Table.SizeBytes
		}
	}
	if entry.Storage != nil && entry.Storage.SizeBytes != nil {
		sizeBytes = entry.Storage.SizeBytes
	}
	if len(summary) > 0 {
		metaattr.UpsertNested(attrs, "type_info", "table", summary)
	}
	if sizeBytes != nil {
		metaattr.SetStorage(attrs, "total_size", *sizeBytes)
	}
	if entry.UpdatedAt != nil {
		updatedAt = entry.UpdatedAt
	}
	refreshed, err := r.repo.UpdateItemByIDWithDepth(tenantID, item.ID, resource.ID, &parent,
		item.ItemType, item.Name, item.FullName, attrs, rowCount, sizeBytes, updatedAt, scanflow.ScanDepthBasic)
	if err != nil {
		return scanprocessor.Result{}, err
	}
	if r.indexer != nil {
		r.indexer.IndexTechnicalMetadata(ctx, resource, tenantID, &parent, refreshed)
	}
	return scanprocessor.Result{Item: refreshed}, nil
}
