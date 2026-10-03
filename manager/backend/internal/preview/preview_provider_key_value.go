package preview

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

// KeyValuePreviewProvider reads a finite native sample of one scanned key.
type KeyValuePreviewProvider struct{}

func NewKeyValuePreviewProvider() PreviewProvider { return &KeyValuePreviewProvider{} }
func (*KeyValuePreviewProvider) Name() string     { return "builtin:key-value" }

func (*KeyValuePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("key preview requires a single bounded sample")
	}
	if req.Page > 1 {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("key preview requires a single bounded sample"))
	}
	reader, ok := req.EnginePlugin.(plugin.KeyValueReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine does not implement KeyValueReadableProvider")
	}
	value, err := reader.ReadKeyValue(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, plugin.KeyValueReadOptions{MaxEntries: req.PageSize, MaxBytes: 1 << 20})
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("missing native key preview")
	}
	return &models.TablePreview{Mode: PreviewModeKeyValue, KeyValue: value, Columns: []string{}, Rows: []map[string]interface{}{}, GeometryColumns: []string{}, EngineID: req.Engine.ID, EngineType: req.Engine.EngineType}, nil
}
