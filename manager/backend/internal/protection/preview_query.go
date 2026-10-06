package protection

import (
	"context"
	"reflect"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
)

type PreviewSourceChecker interface {
	CheckManagerPreviewRead(context.Context, string, []plugin.EngineCatalogPath) error
}

type PreviewQueryProtectionStore interface {
	PrepareQueryProtection(context.Context, int64, plugin.EngineCatalogModelSpec, plugin.PreparedQuery, string, dataprotection.SubjectReference, time.Time) (*dataprotection.PreparedTableProtection, error)
}

// PreviewQueryExecutor keeps source authorization and protection on the exact
// provider plan that is subsequently consumed, under the existing HTTP boundary.
// It never stores the credential in a shared service or substitutes a machine.
func PreviewQueryExecutor(checker PreviewSourceChecker, store PreviewQueryProtectionStore, tenantID, engineID uint, credential string, subject dataprotection.SubjectReference) func(context.Context, plugin.EnginePlugin, plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
	return func(ctx context.Context, provider plugin.EnginePlugin, prepared plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
		model, ok := provider.(plugin.EngineCatalogModelProvider)
		if !ok || prepared == nil || checker == nil || store == nil || tenantID == 0 || engineID == 0 {
			return nil, nil, ErrRequired
		}
		readSet, err := prepared.ReadSet(ctx)
		if err != nil || readSet == nil || len(readSet.Paths) == 0 || len(readSet.Paths) > 200 {
			return nil, nil, ErrRequired
		}
		canonical, err := plugin.NewQueryReadSet(readSet.Paths...)
		if err != nil || !reflect.DeepEqual(readSet, canonical) {
			return nil, nil, ErrRequired
		}
		for _, path := range readSet.Paths {
			if path.EngineID != engineID {
				return nil, nil, ErrRequired
			}
		}
		if err := checker.CheckManagerPreviewRead(ctx, credential, readSet.Paths); err != nil {
			return nil, nil, err
		}
		protection, err := store.PrepareQueryProtection(ctx, int64(tenantID), model.EngineCatalogModel(), prepared, ActionPreview, subject, time.Now().UTC())
		if err != nil || protection == nil || protection.Apply == nil {
			return nil, nil, ErrRequired
		}
		result, err := prepared.Execute(ctx)
		if err != nil {
			return nil, nil, err
		}
		if result == nil {
			return nil, nil, ErrRequired
		}
		return result, protection, nil
	}
}
