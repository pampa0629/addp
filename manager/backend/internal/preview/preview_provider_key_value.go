package preview

import (
	"context"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

// KeyValuePreviewProvider browses a scanned dataset and reads its native members.
type KeyValuePreviewProvider struct{}

func NewKeyValuePreviewProvider() PreviewProvider { return &KeyValuePreviewProvider{} }
func (*KeyValuePreviewProvider) Name() string     { return "builtin:key-value" }
func (*KeyValuePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("keyspace preview requires a dataset")
	}
	if req.Page > 1 {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("keyspace preview uses a cursor"))
	}
	reader, ok := req.EnginePlugin.(plugin.KeyValueReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine does not implement KeyValueReadableProvider")
	}
	opts := req.KeyValueOptions
	opts.MaxEntries, opts.MaxBytes = req.PageSize, 1<<20
	result := &models.TablePreview{Mode: PreviewModeKeyValue, Columns: []string{}, Rows: []map[string]interface{}{}, GeometryColumns: []string{}, EngineID: req.Engine.ID, EngineType: req.Engine.EngineType}
	var err error
	if opts.Key != "" {
		result.KeyValue, err = reader.ReadKeyValue(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, opts)
	} else {
		result.Keyspace, err = reader.ListKeyValues(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, opts)
	}
	if err != nil {
		return nil, err
	}
	if result.KeyValue == nil && result.Keyspace == nil {
		return nil, fmt.Errorf("missing keyspace preview")
	}
	return result, nil
}
